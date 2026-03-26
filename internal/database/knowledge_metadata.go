package database

import (
	"encoding/json"
	"math"
	"strings"
)

// KnowledgeMetadataSummary is the normalized metadata view attached to
// knowledge and vector-knowledge search hits.
type KnowledgeMetadataSummary struct {
	Scope            string   `json:"scope,omitempty"`
	KnowledgeLayer   string   `json:"knowledge_layer,omitempty"`
	SourceWorkspace  string   `json:"source_workspace,omitempty"`
	Source           string   `json:"source,omitempty"`
	SampleType       string   `json:"sample_type,omitempty"`
	SourceConfidence float64  `json:"source_confidence,omitempty"`
	TargetTypes      []string `json:"target_types,omitempty"`
	Labels           []string `json:"labels,omitempty"`
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

func parseKnowledgeMetadataString(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
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
