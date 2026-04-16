package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type securitySQLiteImporter struct {
	db         *sql.DB
	summary    *ImportSummary
	workspace  string
	sourcePath string
	tables     map[string]bool
}

type cweRecord struct {
	ID                  string
	CWENum              int
	Name                string
	Abstraction         string
	Status              string
	Description         string
	ExtendedDescription string
	Likelihood          string
}

type capecRecord struct {
	ID                string
	CAPECNum          int
	Name              string
	Abstraction       string
	Status            string
	Description       string
	Severity          string
	Likelihood        string
	Prerequisites     string
	SkillsRequired    string
	ResourcesRequired string
}

type attackTechniqueRecord struct {
	ID              string
	STIXID          string
	Name            string
	Description     string
	Tactics         string
	Platforms       string
	Detection       string
	IsSubTechnique  int
	ParentTechnique string
	Version         string
}

type agenticThreatRecord struct {
	ID              string
	Name            string
	Description     string
	Source          string
	STRIDE          string
	CWEs            string
	AttackTechs     string
	ATLASTechs      string
	KillChainPhases string
	Severity        string
	RelatedControls string
}

type strideCategoryRecord struct {
	ID               string
	Name             string
	SecurityProperty string
	Description      string
}

type strideMappingRecord struct {
	Category string
	CWEID    string
	Score    float64
	Source   string
	Notes    string
}

type owaspTop10Record struct {
	ID          string
	Year        int
	Name        string
	Description string
	CWECount    int
}

type attackMitigationRecord struct {
	ID          string
	Name        string
	Description string
	Context     string
}

func importSecuritySQLite(ctx context.Context, opts ImportOptions, summary *ImportSummary) (*ImportSummary, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return summary, fmt.Errorf("failed to stat source database: %w", err)
	}
	if info.IsDir() {
		return summary, fmt.Errorf("source path must be a sqlite file")
	}

	db, err := sql.Open("sqlite3", "file:"+opts.Path+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return summary, fmt.Errorf("failed to open source database: %w", err)
	}
	defer func() { _ = db.Close() }()

	importer := &securitySQLiteImporter{
		db:         db,
		summary:    summary,
		workspace:  normalizeWorkspace(opts.Workspace),
		sourcePath: opts.Path,
		tables:     make(map[string]bool),
	}

	if err := importer.loadTablePresence(ctx); err != nil {
		return summary, err
	}
	if err := importer.run(ctx); err != nil {
		return summary, err
	}
	if summary.Documents == 0 {
		return summary, fmt.Errorf("no supported records imported from %s", opts.Path)
	}
	return summary, nil
}

func (s *securitySQLiteImporter) run(ctx context.Context) error {
	var ran bool
	for _, table := range []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "cwe", run: s.importCWEs},
		{name: "capec", run: s.importCAPEC},
		{name: "attack_technique", run: s.importAttackTechniques},
		{name: "agentic_threat", run: s.importAgenticThreats},
		{name: "stride_cwe", run: s.importSTRIDEMappings},
		{name: "owasp_top10", run: s.importOWASPTop10},
	} {
		if !s.tables[table.name] {
			continue
		}
		ran = true
		if err := table.run(ctx); err != nil {
			return err
		}
	}
	if !ran {
		return fmt.Errorf("no supported tables found in %s", s.sourcePath)
	}
	return nil
}

func (s *securitySQLiteImporter) loadTablePresence(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return fmt.Errorf("failed to inspect source tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan source table name: %w", err)
		}
		s.tables[strings.TrimSpace(name)] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed while reading source table names: %w", err)
	}
	return nil
}

func (s *securitySQLiteImporter) importCWEs(ctx context.Context) error {
	mitigations, err := s.loadCWEMitigations(ctx)
	if err != nil {
		return err
	}
	hierarchy, err := s.loadCWEHierarchy(ctx)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, cwe_num, name, COALESCE(abstraction, ''), COALESCE(status, ''), COALESCE(description, ''), COALESCE(extended_description, ''), COALESCE(likelihood_of_exploit, '') FROM cwe ORDER BY cwe_num, id`)
	if err != nil {
		return fmt.Errorf("failed to query cwe records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rec cweRecord
		if err := rows.Scan(&rec.ID, &rec.CWENum, &rec.Name, &rec.Abstraction, &rec.Status, &rec.Description, &rec.ExtendedDescription, &rec.Likelihood); err != nil {
			appendImportError(s.summary, "cwe: failed to scan row: %v", err)
			continue
		}
		content := buildCWEContent(rec, mitigations[rec.ID], hierarchy[rec.ID])
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "cwe",
			"source_id":    rec.ID,
			"labels":       []string{"cwe"},
		}
		s.saveDocument(ctx, "security-sqlite://cwe/"+rec.ID, fmt.Sprintf("%s: %s", rec.ID, rec.Name), content, metadata)
	}
	return rows.Err()
}

func (s *securitySQLiteImporter) importCAPEC(ctx context.Context) error {
	relatedCWEs, err := s.loadSimpleRelationMap(ctx, "capec_cwe", `SELECT capec_id, cwe_id FROM capec_cwe ORDER BY capec_id, cwe_id`)
	if err != nil {
		return err
	}
	relatedATTACK, err := s.loadSimpleRelationMap(ctx, "capec_attack", `SELECT capec_id, attack_id FROM capec_attack ORDER BY capec_id, attack_id`)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, capec_num, name, COALESCE(abstraction, ''), COALESCE(status, ''), COALESCE(description, ''), COALESCE(severity, ''), COALESCE(likelihood_of_attack, ''), COALESCE(prerequisites, ''), COALESCE(skills_required, ''), COALESCE(resources_required, '') FROM capec ORDER BY capec_num, id`)
	if err != nil {
		return fmt.Errorf("failed to query capec records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rec capecRecord
		if err := rows.Scan(&rec.ID, &rec.CAPECNum, &rec.Name, &rec.Abstraction, &rec.Status, &rec.Description, &rec.Severity, &rec.Likelihood, &rec.Prerequisites, &rec.SkillsRequired, &rec.ResourcesRequired); err != nil {
			appendImportError(s.summary, "capec: failed to scan row: %v", err)
			continue
		}
		content := buildCAPECContent(rec, relatedCWEs[rec.ID], relatedATTACK[rec.ID])
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "capec",
			"source_id":    rec.ID,
			"labels":       []string{"capec"},
		}
		s.saveDocument(ctx, "security-sqlite://capec/"+rec.ID, fmt.Sprintf("%s: %s", rec.ID, rec.Name), content, metadata)
	}
	return rows.Err()
}

func (s *securitySQLiteImporter) importAttackTechniques(ctx context.Context) error {
	mitigations, err := s.loadAttackTechniqueMitigations(ctx)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(stix_id, ''), name, COALESCE(description, ''), COALESCE(tactics, ''), COALESCE(platforms, ''), COALESCE(detection, ''), COALESCE(is_subtechnique, 0), COALESCE(parent_technique, ''), COALESCE(version, '') FROM attack_technique ORDER BY id`)
	if err != nil {
		return fmt.Errorf("failed to query attack techniques: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rec attackTechniqueRecord
		if err := rows.Scan(&rec.ID, &rec.STIXID, &rec.Name, &rec.Description, &rec.Tactics, &rec.Platforms, &rec.Detection, &rec.IsSubTechnique, &rec.ParentTechnique, &rec.Version); err != nil {
			appendImportError(s.summary, "attack_technique: failed to scan row: %v", err)
			continue
		}
		content := buildAttackTechniqueContent(rec, mitigations[rec.ID])
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "attack_technique",
			"source_id":    rec.ID,
			"stix_id":      rec.STIXID,
			"labels":       []string{"attack-technique"},
		}
		s.saveDocument(ctx, "security-sqlite://attack_technique/"+rec.ID, fmt.Sprintf("%s: %s", rec.ID, rec.Name), content, metadata)
	}
	return rows.Err()
}

func (s *securitySQLiteImporter) importAgenticThreats(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(description, ''), source, COALESCE(stride_categories, ''), COALESCE(cwes, ''), COALESCE(attack_techniques, ''), COALESCE(atlas_techniques, ''), COALESCE(kill_chain_phases, ''), COALESCE(severity, ''), COALESCE(related_controls, '') FROM agentic_threat ORDER BY id`)
	if err != nil {
		return fmt.Errorf("failed to query agentic threat records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rec agenticThreatRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Description, &rec.Source, &rec.STRIDE, &rec.CWEs, &rec.AttackTechs, &rec.ATLASTechs, &rec.KillChainPhases, &rec.Severity, &rec.RelatedControls); err != nil {
			appendImportError(s.summary, "agentic_threat: failed to scan row: %v", err)
			continue
		}
		content := buildAgenticThreatContent(rec)
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "agentic_threat",
			"source_id":    rec.ID,
			"labels":       []string{"agentic-threat"},
		}
		s.saveDocument(ctx, "security-sqlite://agentic_threat/"+rec.ID, fmt.Sprintf("%s: %s", rec.ID, rec.Name), content, metadata)
	}
	return rows.Err()
}

func (s *securitySQLiteImporter) importSTRIDEMappings(ctx context.Context) error {
	categories, err := s.loadStrideCategories(ctx)
	if err != nil {
		return err
	}
	cweNames, err := s.loadNameMap(ctx, "cwe", `SELECT id, name FROM cwe`)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT stride_category, cwe_id, COALESCE(relevance_score, 0), COALESCE(source, ''), COALESCE(notes, '') FROM stride_cwe ORDER BY stride_category, relevance_score DESC, cwe_id`)
	if err != nil {
		return fmt.Errorf("failed to query stride_cwe records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	grouped := make(map[string][]strideMappingRecord)
	for rows.Next() {
		var rec strideMappingRecord
		if err := rows.Scan(&rec.Category, &rec.CWEID, &rec.Score, &rec.Source, &rec.Notes); err != nil {
			appendImportError(s.summary, "stride_cwe: failed to scan row: %v", err)
			continue
		}
		grouped[rec.Category] = append(grouped[rec.Category], rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	categoriesOrdered := orderedKeys(grouped)
	for _, categoryID := range categoriesOrdered {
		category := categories[categoryID]
		content := buildSTRIDEContent(categoryID, category, grouped[categoryID], cweNames)
		title := fmt.Sprintf("STRIDE %s", categoryID)
		if strings.TrimSpace(category.Name) != "" {
			title = fmt.Sprintf("STRIDE %s: %s", categoryID, category.Name)
		}
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "stride_cwe",
			"source_id":    categoryID,
			"labels":       []string{"stride"},
		}
		s.saveDocument(ctx, "security-sqlite://stride_cwe/"+categoryID, title, content, metadata)
	}
	return nil
}

func (s *securitySQLiteImporter) importOWASPTop10(ctx context.Context) error {
	relatedCWEs, err := s.loadOWASPCWEs(ctx)
	if err != nil {
		return err
	}
	cweNames, err := s.loadNameMap(ctx, "cwe", `SELECT id, name FROM cwe`)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, year, name, COALESCE(description, ''), COALESCE(cwe_count, 0) FROM owasp_top10 ORDER BY year DESC, id`)
	if err != nil {
		return fmt.Errorf("failed to query owasp_top10 records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rec owaspTop10Record
		if err := rows.Scan(&rec.ID, &rec.Year, &rec.Name, &rec.Description, &rec.CWECount); err != nil {
			appendImportError(s.summary, "owasp_top10: failed to scan row: %v", err)
			continue
		}
		lookupKey := fmt.Sprintf("%s::%d", rec.ID, rec.Year)
		content := buildOWASPTop10Content(rec, relatedCWEs[lookupKey], cweNames)
		metadata := map[string]interface{}{
			"import_type":  "security-sqlite",
			"source_db":    s.sourcePath,
			"source_table": "owasp_top10",
			"source_id":    lookupKey,
			"labels":       []string{"owasp-top10"},
		}
		s.saveDocument(ctx, fmt.Sprintf("security-sqlite://owasp_top10/%s@%d", rec.ID, rec.Year), fmt.Sprintf("%s (%d): %s", rec.ID, rec.Year, rec.Name), content, metadata)
	}
	return rows.Err()
}

func (s *securitySQLiteImporter) saveDocument(ctx context.Context, sourcePath, title, content string, metadata map[string]interface{}) {
	chunkCount, err := saveKnowledgeContent(ctx, s.workspace, sourcePath, "sqlite-import", "md", title, content, metadata, true)
	if err != nil {
		appendImportError(s.summary, "%s: %v", sourcePath, err)
		return
	}
	s.summary.Documents++
	s.summary.Chunks += chunkCount
}

func (s *securitySQLiteImporter) loadCWEMitigations(ctx context.Context) (map[string][]string, error) {
	if !s.tables["cwe_mitigation"] {
		return map[string][]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT cwe_id, COALESCE(phase, ''), COALESCE(strategy, ''), description, COALESCE(effectiveness, '') FROM cwe_mitigation ORDER BY cwe_id, id`)
	if err != nil {
		return nil, fmt.Errorf("failed to query cwe mitigations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]string)
	for rows.Next() {
		var cweID, phase, strategy, description, effectiveness string
		if err := rows.Scan(&cweID, &phase, &strategy, &description, &effectiveness); err != nil {
			return nil, fmt.Errorf("failed to scan cwe mitigation: %w", err)
		}
		var prefix []string
		if phase != "" {
			prefix = append(prefix, phase)
		}
		if strategy != "" {
			prefix = append(prefix, strategy)
		}
		if effectiveness != "" {
			prefix = append(prefix, effectiveness)
		}
		entry := strings.TrimSpace(description)
		if len(prefix) > 0 {
			entry = fmt.Sprintf("[%s] %s", strings.Join(prefix, "/"), entry)
		}
		result[cweID] = append(result[cweID], entry)
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadCWEHierarchy(ctx context.Context) (map[string][]string, error) {
	if !s.tables["cwe_hierarchy"] {
		return map[string][]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT child_id, parent_id, COALESCE(nature, ''), COALESCE(ordinal, '') FROM cwe_hierarchy ORDER BY child_id, parent_id`)
	if err != nil {
		return nil, fmt.Errorf("failed to query cwe hierarchy: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]string)
	for rows.Next() {
		var childID, parentID, nature, ordinal string
		if err := rows.Scan(&childID, &parentID, &nature, &ordinal); err != nil {
			return nil, fmt.Errorf("failed to scan cwe hierarchy: %w", err)
		}
		entry := strings.TrimSpace(parentID)
		var annotations []string
		if nature != "" {
			annotations = append(annotations, nature)
		}
		if ordinal != "" {
			annotations = append(annotations, ordinal)
		}
		if len(annotations) > 0 {
			entry = fmt.Sprintf("%s (%s)", entry, strings.Join(annotations, ", "))
		}
		result[childID] = append(result[childID], entry)
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadSimpleRelationMap(ctx context.Context, table, query string) (map[string][]string, error) {
	if !s.tables[table] {
		return map[string][]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s relations: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan %s relation: %w", table, err)
		}
		result[key] = append(result[key], strings.TrimSpace(value))
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadAttackTechniqueMitigations(ctx context.Context) (map[string][]attackMitigationRecord, error) {
	if !s.tables["attack_tech_mitigation"] || !s.tables["attack_mitigation"] {
		return map[string][]attackMitigationRecord{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT atm.technique_id, am.id, am.name, COALESCE(am.description, ''), COALESCE(atm.description, '') FROM attack_tech_mitigation atm JOIN attack_mitigation am ON am.id = atm.mitigation_id ORDER BY atm.technique_id, am.id`)
	if err != nil {
		return nil, fmt.Errorf("failed to query attack technique mitigations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]attackMitigationRecord)
	for rows.Next() {
		var techniqueID string
		var rec attackMitigationRecord
		if err := rows.Scan(&techniqueID, &rec.ID, &rec.Name, &rec.Description, &rec.Context); err != nil {
			return nil, fmt.Errorf("failed to scan attack technique mitigation: %w", err)
		}
		result[techniqueID] = append(result[techniqueID], rec)
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadStrideCategories(ctx context.Context) (map[string]strideCategoryRecord, error) {
	if !s.tables["stride_category"] {
		return map[string]strideCategoryRecord{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, security_property, COALESCE(description, '') FROM stride_category ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("failed to query stride categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]strideCategoryRecord)
	for rows.Next() {
		var rec strideCategoryRecord
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.SecurityProperty, &rec.Description); err != nil {
			return nil, fmt.Errorf("failed to scan stride category: %w", err)
		}
		result[rec.ID] = rec
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadNameMap(ctx context.Context, table, query string) (map[string]string, error) {
	if !s.tables[table] {
		return map[string]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s names: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan %s name: %w", table, err)
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result, rows.Err()
}

func (s *securitySQLiteImporter) loadOWASPCWEs(ctx context.Context) (map[string][]string, error) {
	if !s.tables["owasp_cwe"] {
		return map[string][]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT owasp_id, year, cwe_id FROM owasp_cwe ORDER BY year DESC, owasp_id, cwe_id`)
	if err != nil {
		return nil, fmt.Errorf("failed to query owasp_cwe relations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]string)
	for rows.Next() {
		var owaspID, cweID string
		var year int
		if err := rows.Scan(&owaspID, &year, &cweID); err != nil {
			return nil, fmt.Errorf("failed to scan owasp_cwe relation: %w", err)
		}
		key := fmt.Sprintf("%s::%d", owaspID, year)
		result[key] = append(result[key], strings.TrimSpace(cweID))
	}
	return result, rows.Err()
}

func buildCWEContent(rec cweRecord, mitigations, hierarchy []string) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(rec.ID)
	builder.WriteString(": ")
	builder.WriteString(strings.TrimSpace(rec.Name))
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	builder.WriteString(fmt.Sprintf("- CWE Number: %d\n", rec.CWENum))
	writeBullet(&builder, "Abstraction", rec.Abstraction)
	writeBullet(&builder, "Status", rec.Status)
	writeBullet(&builder, "Likelihood of Exploit", rec.Likelihood)
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", rec.Description)
	writeParagraphSection(&builder, "Extended Description", rec.ExtendedDescription)
	writeListSection(&builder, "Mitigations", mitigations)
	writeListSection(&builder, "Hierarchy", hierarchy)
	return builder.String()
}

func buildCAPECContent(rec capecRecord, cwes, attacks []string) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(rec.ID)
	builder.WriteString(": ")
	builder.WriteString(strings.TrimSpace(rec.Name))
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	builder.WriteString(fmt.Sprintf("- CAPEC Number: %d\n", rec.CAPECNum))
	writeBullet(&builder, "Abstraction", rec.Abstraction)
	writeBullet(&builder, "Status", rec.Status)
	writeBullet(&builder, "Severity", rec.Severity)
	writeBullet(&builder, "Likelihood of Attack", rec.Likelihood)
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", rec.Description)
	writeListSection(&builder, "Prerequisites", parseDelimitedList(rec.Prerequisites))
	writeListSection(&builder, "Skills Required", parseDelimitedList(rec.SkillsRequired))
	writeListSection(&builder, "Resources Required", parseDelimitedList(rec.ResourcesRequired))
	writeListSection(&builder, "Related CWE", cwes)
	writeListSection(&builder, "Related ATT&CK", attacks)
	return builder.String()
}

func buildAttackTechniqueContent(rec attackTechniqueRecord, mitigations []attackMitigationRecord) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(rec.ID)
	builder.WriteString(": ")
	builder.WriteString(strings.TrimSpace(rec.Name))
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	writeBullet(&builder, "STIX ID", rec.STIXID)
	writeBullet(&builder, "Tactics", strings.Join(parseDelimitedList(rec.Tactics), ", "))
	writeBullet(&builder, "Platforms", strings.Join(parseDelimitedList(rec.Platforms), ", "))
	if rec.IsSubTechnique > 0 {
		writeBullet(&builder, "Sub-technique", "true")
	}
	writeBullet(&builder, "Parent Technique", rec.ParentTechnique)
	writeBullet(&builder, "Version", rec.Version)
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", rec.Description)
	writeParagraphSection(&builder, "Detection", rec.Detection)
	if len(mitigations) > 0 {
		builder.WriteString("## Mitigations\n")
		for _, mitigation := range mitigations {
			text := fmt.Sprintf("%s: %s", mitigation.ID, mitigation.Name)
			details := firstNonEmptyString(strings.TrimSpace(mitigation.Context), strings.TrimSpace(mitigation.Description))
			if details != "" {
				text += " — " + details
			}
			builder.WriteString("- ")
			builder.WriteString(text)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildAgenticThreatContent(rec agenticThreatRecord) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(rec.ID)
	builder.WriteString(": ")
	builder.WriteString(strings.TrimSpace(rec.Name))
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	writeBullet(&builder, "Source", rec.Source)
	writeBullet(&builder, "Severity", rec.Severity)
	writeBullet(&builder, "STRIDE Categories", strings.Join(parseDelimitedList(rec.STRIDE), ", "))
	writeBullet(&builder, "Related CWE", strings.Join(parseDelimitedList(rec.CWEs), ", "))
	writeBullet(&builder, "Related ATT&CK", strings.Join(parseDelimitedList(rec.AttackTechs), ", "))
	writeBullet(&builder, "ATLAS Techniques", strings.Join(parseDelimitedList(rec.ATLASTechs), ", "))
	writeBullet(&builder, "Kill Chain Phases", strings.Join(parseDelimitedList(rec.KillChainPhases), ", "))
	writeBullet(&builder, "Related Controls", strings.Join(parseDelimitedList(rec.RelatedControls), ", "))
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", rec.Description)
	return builder.String()
}

func buildSTRIDEContent(categoryID string, category strideCategoryRecord, mappings []strideMappingRecord, cweNames map[string]string) string {
	var builder strings.Builder
	title := strings.TrimSpace(category.Name)
	if title == "" {
		title = categoryID
	}
	builder.WriteString("# STRIDE ")
	builder.WriteString(categoryID)
	builder.WriteString(": ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	writeBullet(&builder, "Security Property", category.SecurityProperty)
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", category.Description)
	if len(mappings) > 0 {
		builder.WriteString("## Related CWE\n")
		for _, mapping := range mappings {
			text := mapping.CWEID
			if name := strings.TrimSpace(cweNames[mapping.CWEID]); name != "" {
				text += " — " + name
			}
			if mapping.Score > 0 {
				text += fmt.Sprintf(" (score: %s)", strconv.FormatFloat(mapping.Score, 'f', 2, 64))
			}
			if source := strings.TrimSpace(mapping.Source); source != "" {
				text += "; source: " + source
			}
			if notes := strings.TrimSpace(mapping.Notes); notes != "" {
				text += "; notes: " + notes
			}
			builder.WriteString("- ")
			builder.WriteString(text)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildOWASPTop10Content(rec owaspTop10Record, cwes []string, cweNames map[string]string) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(rec.ID)
	builder.WriteString(" (")
	builder.WriteString(strconv.Itoa(rec.Year))
	builder.WriteString("): ")
	builder.WriteString(strings.TrimSpace(rec.Name))
	builder.WriteString("\n\n")
	builder.WriteString("## Overview\n")
	writeBullet(&builder, "Year", strconv.Itoa(rec.Year))
	writeBullet(&builder, "CWE Count", strconv.Itoa(rec.CWECount))
	builder.WriteString("\n")
	writeParagraphSection(&builder, "Description", rec.Description)
	if len(cwes) > 0 {
		builder.WriteString("## Related CWE\n")
		for _, cweID := range cwes {
			text := cweID
			if name := strings.TrimSpace(cweNames[cweID]); name != "" {
				text += " — " + name
			}
			builder.WriteString("- ")
			builder.WriteString(text)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func writeBullet(builder *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(label)
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteString("\n")
}

func writeParagraphSection(builder *strings.Builder, title, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n")
	builder.WriteString(value)
	builder.WriteString("\n\n")
}

func writeListSection(builder *strings.Builder, title string, items []string) {
	items = compactStrings(items)
	if len(items) == 0 {
		return
	}
	builder.WriteString("## ")
	builder.WriteString(title)
	builder.WriteString("\n")
	for _, item := range items {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
}

func parseDelimitedList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var jsonValues []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &jsonValues) == nil {
		return compactStrings(jsonValues)
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case '\n', ';':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return nil
	}
	return compactStrings(parts)
}

func compactStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		normalized := strings.TrimSpace(item)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func orderedKeys[T any](input map[string]T) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
