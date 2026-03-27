package database

import (
	"encoding/json"
	"math"
	"strings"
)

// KnowledgeMetadataSummary is the normalized metadata view attached to
// knowledge and vector-knowledge search hits.
type KnowledgeMetadataSummary struct {
	Scope                string   `json:"scope,omitempty"`
	KnowledgeLayer       string   `json:"knowledge_layer,omitempty"`
	SourceWorkspace      string   `json:"source_workspace,omitempty"`
	Source               string   `json:"source,omitempty"`
	SampleType           string   `json:"sample_type,omitempty"`
	SourceConfidence     float64  `json:"source_confidence,omitempty"`
	RetrievalFingerprint string   `json:"retrieval_fingerprint,omitempty"`
	TargetTypes          []string `json:"target_types,omitempty"`
	Labels               []string `json:"labels,omitempty"`
}

// IsZero reports whether the metadata summary is effectively empty.
func (m *KnowledgeMetadataSummary) IsZero() bool {
	if m == nil {
		return true
	}
	return strings.TrimSpace(m.Scope) == "" &&
		strings.TrimSpace(m.KnowledgeLayer) == "" &&
		strings.TrimSpace(m.SourceWorkspace) == "" &&
		strings.TrimSpace(m.Source) == "" &&
		strings.TrimSpace(m.SampleType) == "" &&
		m.SourceConfidence == 0 &&
		strings.TrimSpace(m.RetrievalFingerprint) == "" &&
		len(m.TargetTypes) == 0 &&
		len(m.Labels) == 0
}

// ParseKnowledgeMetadata extracts a normalized metadata summary from one or more
// metadata JSON payloads. Later payloads only fill missing fields.
func ParseKnowledgeMetadata(raw ...string) *KnowledgeMetadataSummary {
	summary := &KnowledgeMetadataSummary{}
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(item), &parsed); err != nil {
			continue
		}

		if summary.Scope == "" {
			summary.Scope = parseKnowledgeMetadataString(parsed["scope"])
		}
		if summary.KnowledgeLayer == "" {
			summary.KnowledgeLayer = parseKnowledgeMetadataString(parsed["knowledge_layer"])
		}
		if summary.SourceWorkspace == "" {
			summary.SourceWorkspace = parseKnowledgeMetadataString(parsed["source_workspace"])
		}
		if summary.Source == "" {
			summary.Source = parseKnowledgeMetadataString(parsed["source"])
		}
		if summary.SampleType == "" {
			summary.SampleType = parseKnowledgeMetadataString(parsed["sample_type"])
		}
		if summary.SourceConfidence == 0 {
			summary.SourceConfidence = parseKnowledgeMetadataFloat(parsed["source_confidence"])
		}
		if summary.RetrievalFingerprint == "" {
			summary.RetrievalFingerprint = parseKnowledgeMetadataString(
				firstKnowledgeMetadataValue(parsed, "retrieval_fingerprint", "content_fingerprint", "fingerprint", "fingerprint_key"),
			)
		}
		if len(summary.TargetTypes) == 0 {
			summary.TargetTypes = parseKnowledgeMetadataStringSlice(parsed["target_types"])
		}
		if len(summary.Labels) == 0 {
			summary.Labels = parseKnowledgeMetadataStringSlice(parsed["labels"])
		}
	}

	if summary.IsZero() {
		return nil
	}
	return summary
}

// ComputeKnowledgeMetadataBoost applies practical retrieval weighting to
// normalized knowledge metadata so operationally useful memory ranks higher.
func ComputeKnowledgeMetadataBoost(query string, metadata *KnowledgeMetadataSummary) float64 {
	if metadata == nil {
		return 0
	}

	boost := metadata.SourceConfidence * 0.18
	switch normalizeKnowledgeIntentText(metadata.SampleType) {
	case "verified":
		boost += 0.22
	case "workspace summary":
		boost += 0.05
	case "ai analysis":
		boost -= 0.03
	case "false positive":
		if queryLooksLikeFalsePositiveIntent(query) {
			boost += 0.08
		} else {
			boost -= 0.14
		}
	case "operator followup":
		boost += 0.12
	}

	switch strings.ToLower(strings.TrimSpace(metadata.Scope)) {
	case "workspace":
		boost += 0.08
	case "project":
		boost += 0.04
	case "public":
		boost += 0.03
	}

	for _, targetType := range metadata.TargetTypes {
		if queryMentionsKnowledgeToken(query, targetType) {
			boost += 0.06
			break
		}
	}

	for _, label := range metadata.Labels {
		if queryMentionsKnowledgeToken(query, label) {
			boost += 0.05
			break
		}
	}

	if metadataHasOperationalSignals(metadata) {
		boost += 0.04
		if queryLooksLikeOperationalIntent(query) {
			boost += 0.18
		}
	}

	return boost
}

// KnowledgeMetadataFingerprint returns the best available stable fingerprint for
// cross-layer and cross-document deduplication.
func KnowledgeMetadataFingerprint(metadata *KnowledgeMetadataSummary) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(metadata.RetrievalFingerprint)
}

// KnowledgeMetadataMatchesFilters applies optional retrieval filters without
// excluding plain ingested documents that do not provide learned metadata.
func KnowledgeMetadataMatchesFilters(metadata *KnowledgeMetadataSummary, minSourceConfidence float64, sampleTypes, excludeSampleTypes []string) bool {
	if metadata == nil {
		return len(normalizeKnowledgeTokens(sampleTypes)) == 0
	}

	if minSourceConfidence > 0 && metadata.SourceConfidence > 0 && metadata.SourceConfidence < minSourceConfidence {
		return false
	}

	normalizedSampleType := normalizeKnowledgeIntentText(metadata.SampleType)
	allowed := normalizeKnowledgeTokens(sampleTypes)
	if len(allowed) > 0 {
		if normalizedSampleType == "" {
			return false
		}
		if _, ok := allowed[normalizedSampleType]; !ok {
			return false
		}
	}

	blocked := normalizeKnowledgeTokens(excludeSampleTypes)
	if len(blocked) > 0 {
		if _, ok := blocked[normalizedSampleType]; ok {
			return false
		}
	}

	return true
}

func parseKnowledgeMetadataString(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstKnowledgeMetadataValue(parsed map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := parsed[key]; ok {
			return value
		}
	}
	return nil
}

func parseKnowledgeMetadataFloat(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0
		}
		return typed
	case float32:
		number := float64(typed)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return 0
		}
		return number
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		number, err := typed.Float64()
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return 0
		}
		return number
	default:
		return 0
	}
}

func parseKnowledgeMetadataStringSlice(value interface{}) []string {
	list, ok := value.([]interface{})
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(list))
	result := make([]string, 0, len(list))
	for _, item := range list {
		text, _ := item.(string)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func metadataHasOperationalSignals(metadata *KnowledgeMetadataSummary) bool {
	if metadata == nil {
		return false
	}
	if normalizeKnowledgeIntentText(metadata.SampleType) == "operator followup" {
		return true
	}
	for _, label := range metadata.Labels {
		switch normalizeKnowledgeIntentText(label) {
		case "operator followup", "manual followup", "operator queue", "retest plan", "followup decision", "campaign handoff":
			return true
		}
	}
	return false
}

func queryLooksLikeFalsePositiveIntent(query string) bool {
	query = normalizeKnowledgeIntentText(query)
	for _, token := range []string{"false positive", "误报", "noise"} {
		if strings.Contains(query, normalizeKnowledgeIntentText(token)) {
			return true
		}
	}
	return false
}

func queryLooksLikeOperationalIntent(query string) bool {
	query = normalizeKnowledgeIntentText(query)
	for _, token := range []string{
		"retest", "复测", "manual", "人工", "followup", "follow up", "campaign", "handoff",
		"operator", "queue", "exploit", "exploitation", "takeover", "auth bypass",
		"privilege escalation", "pivot", "evidence", "proof",
	} {
		if strings.Contains(query, normalizeKnowledgeIntentText(token)) {
			return true
		}
	}
	return false
}

func queryMentionsKnowledgeToken(query, token string) bool {
	query = normalizeKnowledgeIntentText(query)
	token = normalizeKnowledgeIntentText(token)
	return query != "" && token != "" && strings.Contains(query, token)
}

func normalizeKnowledgeTokens(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			token := normalizeKnowledgeIntentText(part)
			if token == "" {
				continue
			}
			result[token] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeKnowledgeIntentText(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	replacer := strings.NewReplacer("_", " ", "-", " ", "/", " ", ":", " ")
	input = replacer.Replace(input)
	return strings.Join(strings.Fields(input), " ")
}
