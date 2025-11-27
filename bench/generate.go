// Package bench provides HTML report generation for benchmarks.
package bench

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
)

// GenerateHTMLReport generates an HTML report from benchmark results.
func GenerateHTMLReport(templatePath, outputPath string, benchmarkOutput []byte) error {
	// Read template
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template: %w", err)
	}

	// Get system info
	sysInfo := GetSystemInfo()

	// Parse benchmark results
	results := ParseBenchmarkOutput(string(benchmarkOutput))

	// Generate content sections
	var content strings.Builder

	// System Information Section
	content.WriteString(generateSystemInfo(sysInfo))

	// Executive Summary
	content.WriteString(generateExecutiveSummary(results))

	// Crypto Comparison Section
	content.WriteString(generateCryptoComparison(results))

	// QC Operations Section
	content.WriteString(generateQCSection(results))

	// Performance Summary
	content.WriteString(generatePerformanceSummary(results))

	// Detailed Benchmarks
	content.WriteString(generateDetailedBenchmarks(results))

	// Replace {{CONTENT}} in template
	html := string(tmplContent)
	html = strings.Replace(html, "{{CONTENT}}", content.String(), 1)

	// Write output
	return os.WriteFile(outputPath, []byte(html), 0644)
}

func generateSystemInfo(sysInfo *SystemInfo) string {
	return `<div class="section">
    <h2>System Information</h2>
    <div class="info-grid">
        <div class="info-item">
            <label>Timestamp</label>
            <value>` + sysInfo.Timestamp + `</value>
        </div>
        <div class="info-item">
            <label>Operating System</label>
            <value>` + sysInfo.OS + `</value>
        </div>
        <div class="info-item">
            <label>Architecture</label>
            <value>` + sysInfo.Architecture + `</value>
        </div>
        <div class="info-item">
            <label>Go Version</label>
            <value>` + sysInfo.GoVersion + `</value>
        </div>
    </div>
    <div class="info-grid">
        <div class="info-item" style="grid-column: 1 / -1;">
            <label>CPU</label>
            <value>` + sysInfo.CPU + `</value>
        </div>
    </div>
</div>`
}

func generateExecutiveSummary(results []BenchmarkResult) string {
	var buf bytes.Buffer

	// Calculate key metrics
	ed25519Sign := findBenchmark(results, "Ed25519Sign-")
	ed25519Verify := findBenchmark(results, "Ed25519Verify-")
	blsVerify := findBenchmark(results, "BLSVerify-")
	blsAggregate := findBenchmark(results, "BLSAggregate-")
	qcFormation4 := findBenchmark(results, "QCCreationAndValidation_4Nodes/QCFormation")
	qcFormation7 := findBenchmark(results, "QCCreationAndValidation_7Nodes/QCFormation")

	buf.WriteString(`<div class="section">
    <h2>Executive Summary</h2>
    <p style="color: var(--text-secondary); margin-bottom: 1.5rem;">
        HotStuff-2 is an optimal two-phase responsive BFT consensus protocol. This benchmark report 
        measures cryptographic operations, QC (Quorum Certificate) formation, and consensus throughput.
    </p>
    <div class="summary-grid">`)

	// Key findings
	buf.WriteString(`<div class="summary-card">
            <div class="summary-icon">⚡</div>
            <div class="summary-title">Crypto Performance</div>
            <div class="summary-value">`)

	if ed25519Sign != nil {
		buf.WriteString(fmt.Sprintf("Ed25519: %s ops/sec sign", FormatOpsPerSec(ed25519Sign.NsPerOp)))
	}
	buf.WriteString(`</div>
        </div>`)

	buf.WriteString(`<div class="summary-card">
            <div class="summary-icon">🔗</div>
            <div class="summary-title">QC Formation</div>
            <div class="summary-value">`)

	if qcFormation4 != nil {
		buf.WriteString(fmt.Sprintf("4 nodes: %s", FormatDuration(qcFormation4.NsPerOp)))
	}
	if qcFormation7 != nil {
		buf.WriteString(fmt.Sprintf("<br>7 nodes: %s", FormatDuration(qcFormation7.NsPerOp)))
	}
	buf.WriteString(`</div>
        </div>`)

	buf.WriteString(`<div class="summary-card">
            <div class="summary-icon">📦</div>
            <div class="summary-title">BLS Aggregation</div>
            <div class="summary-value">`)

	if blsAggregate != nil {
		buf.WriteString(fmt.Sprintf("%s ops/sec", FormatOpsPerSec(blsAggregate.NsPerOp)))
		buf.WriteString("<br>Constant-size signatures")
	} else {
		buf.WriteString("Not benchmarked")
	}
	buf.WriteString(`</div>
        </div>`)

	// Recommendation card
	buf.WriteString(`<div class="summary-card recommendation">
            <div class="summary-icon">💡</div>
            <div class="summary-title">Recommendation</div>
            <div class="summary-value">`)

	if ed25519Verify != nil && blsVerify != nil {
		ed25519OpsPerSec := 1000000000 / ed25519Verify.NsPerOp
		blsOpsPerSec := 1000000000 / blsVerify.NsPerOp
		ratio := ed25519OpsPerSec / blsOpsPerSec

		if ratio > 10 {
			buf.WriteString(fmt.Sprintf("Ed25519 is %.0fx faster for verification.<br>Use BLS for 25+ validators.", ratio))
		} else {
			buf.WriteString("Use Ed25519 for small networks,<br>BLS for large networks (25+).")
		}
	} else {
		buf.WriteString("Use Ed25519 for small networks,<br>BLS for large networks.")
	}

	buf.WriteString(`</div>
        </div>`)

	buf.WriteString(`</div></div>`)
	return buf.String()
}

func generateCryptoComparison(results []BenchmarkResult) string {
	var buf bytes.Buffer

	// Find relevant benchmarks - use correct naming patterns
	ed25519Sign := findBenchmark(results, "Ed25519Sign-")
	ed25519Verify := findBenchmark(results, "Ed25519Verify-")
	ed25519KeyGen := findBenchmark(results, "Ed25519KeyGeneration-")
	blsSign := findBenchmark(results, "BLSSign-")
	blsVerify := findBenchmark(results, "BLSVerify-")
	blsKeyGen := findBenchmark(results, "BLSKeyGeneration-")
	blsAggregate := findBenchmark(results, "BLSAggregate-")
	blsAggregateVerify := findBenchmark(results, "BLSAggregateVerify-")

	buf.WriteString(`<div class="section">
    <h2>Cryptographic Scheme Comparison</h2>
    <p style="color: var(--text-secondary); margin-bottom: 1.5rem;">
        Performance comparison of Ed25519 (standard signatures) and BLS12-381 (aggregatable signatures).
        Ed25519 is faster per-operation, while BLS enables constant-size aggregate signatures.
    </p>
    <table>
        <thead>
            <tr>
                <th>Operation</th>
                <th style="text-align: right;">Ed25519</th>
                <th style="text-align: right;">BLS12-381</th>
                <th style="text-align: right;">Ratio</th>
                <th>Notes</th>
            </tr>
        </thead>
        <tbody>`)

	// Key Generation
	if ed25519KeyGen != nil && blsKeyGen != nil {
		ratio := blsKeyGen.NsPerOp / ed25519KeyGen.NsPerOp
		winner := "Ed25519"
		if ratio < 1 {
			winner = "BLS"
			ratio = 1 / ratio
		}
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Key Generation</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%.1fx</td>
                <td><span class="badge badge-crypto">%s faster</span></td>
            </tr>`,
			ClassifySpeed(ed25519KeyGen.NsPerOp), FormatDuration(ed25519KeyGen.NsPerOp),
			ClassifySpeed(blsKeyGen.NsPerOp), FormatDuration(blsKeyGen.NsPerOp),
			ratio, winner))
	}

	// Signing
	if ed25519Sign != nil && blsSign != nil {
		ratio := blsSign.NsPerOp / ed25519Sign.NsPerOp
		winner := "Ed25519"
		if ratio < 1 {
			winner = "BLS"
			ratio = 1 / ratio
		}
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Sign</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%.1fx</td>
                <td><span class="badge badge-crypto">%s faster</span></td>
            </tr>`,
			ClassifySpeed(ed25519Sign.NsPerOp), FormatDuration(ed25519Sign.NsPerOp),
			ClassifySpeed(blsSign.NsPerOp), FormatDuration(blsSign.NsPerOp),
			ratio, winner))
	}

	// Verification
	if ed25519Verify != nil && blsVerify != nil {
		ratio := blsVerify.NsPerOp / ed25519Verify.NsPerOp
		winner := "Ed25519"
		if ratio < 1 {
			winner = "BLS"
			ratio = 1 / ratio
		}
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Verify</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%.1fx</td>
                <td><span class="badge badge-crypto">%s faster</span></td>
            </tr>`,
			ClassifySpeed(ed25519Verify.NsPerOp), FormatDuration(ed25519Verify.NsPerOp),
			ClassifySpeed(blsVerify.NsPerOp), FormatDuration(blsVerify.NsPerOp),
			ratio, winner))
	}

	// BLS-specific operations
	if blsAggregate != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Aggregate (7 sigs)</strong></td>
                <td style="text-align: right; color: var(--text-secondary);">N/A</td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">-</td>
                <td><span class="badge badge-qc">BLS only</span></td>
            </tr>`,
			ClassifySpeed(blsAggregate.NsPerOp), FormatDuration(blsAggregate.NsPerOp)))
	}

	if blsAggregateVerify != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Aggregate Verify</strong></td>
                <td style="text-align: right; color: var(--text-secondary);">N/A</td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">-</td>
                <td><span class="badge badge-qc">BLS only</span></td>
            </tr>`,
			ClassifySpeed(blsAggregateVerify.NsPerOp), FormatDuration(blsAggregateVerify.NsPerOp)))
	}

	buf.WriteString(`</tbody>
    </table>`)

	// Bandwidth comparison
	buf.WriteString(`
    <div class="comparison-note">
        <h4>Bandwidth Comparison (7 validators, 2f+1=5 signatures)</h4>
        <div class="bandwidth-grid">
            <div class="bandwidth-item">
                <span class="bandwidth-label">Ed25519 QC Size</span>
                <span class="bandwidth-value">~384 bytes</span>
                <span class="bandwidth-detail">5 × 64 byte signatures + metadata</span>
            </div>
            <div class="bandwidth-item">
                <span class="bandwidth-label">BLS QC Size</span>
                <span class="bandwidth-value">~96 bytes</span>
                <span class="bandwidth-detail">1 × 96 byte aggregate signature</span>
            </div>
            <div class="bandwidth-item highlight">
                <span class="bandwidth-label">Bandwidth Savings</span>
                <span class="bandwidth-value">75%</span>
                <span class="bandwidth-detail">~288 bytes saved per QC</span>
            </div>
        </div>
    </div>
</div>`)

	return buf.String()
}

func generateQCSection(results []BenchmarkResult) string {
	var buf bytes.Buffer

	// Find QC benchmarks
	qcFormation4 := findBenchmark(results, "QCCreationAndValidation_4Nodes/QCFormation")
	qcValidation4 := findBenchmark(results, "QCCreationAndValidation_4Nodes/QCValidation")
	qcFormation7 := findBenchmark(results, "QCCreationAndValidation_7Nodes/QCFormation")
	qcValidation7 := findBenchmark(results, "QCCreationAndValidation_7Nodes/QCValidation")
	voteAggregation := findBenchmark(results, "VoteAggregation-")
	qcFormationStandalone := findBenchmark(results, "QCFormation-")

	if qcFormation4 == nil && qcFormation7 == nil && voteAggregation == nil {
		return "" // No QC benchmarks found
	}

	buf.WriteString(`<div class="section">
    <h2>Quorum Certificate Operations</h2>
    <p style="color: var(--text-secondary); margin-bottom: 1.5rem;">
        QC formation collects 2f+1 votes and creates a certificate. QC validation verifies all signatures.
        These are the core operations in HotStuff-2 consensus.
    </p>
    <table>
        <thead>
            <tr>
                <th>Operation</th>
                <th style="text-align: right;">Time</th>
                <th style="text-align: right;">Ops/sec</th>
                <th style="text-align: right;">Memory</th>
                <th style="text-align: right;">Allocs</th>
            </tr>
        </thead>
        <tbody>`)

	if qcFormation4 != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>QC Formation (4 nodes)</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(qcFormation4.NsPerOp), FormatDuration(qcFormation4.NsPerOp),
			FormatOpsPerSec(qcFormation4.NsPerOp),
			qcFormation4.BytesPerOp, qcFormation4.AllocsPerOp))
	}

	if qcValidation4 != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>QC Validation (4 nodes)</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(qcValidation4.NsPerOp), FormatDuration(qcValidation4.NsPerOp),
			FormatOpsPerSec(qcValidation4.NsPerOp),
			qcValidation4.BytesPerOp, qcValidation4.AllocsPerOp))
	}

	if qcFormation7 != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>QC Formation (7 nodes)</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(qcFormation7.NsPerOp), FormatDuration(qcFormation7.NsPerOp),
			FormatOpsPerSec(qcFormation7.NsPerOp),
			qcFormation7.BytesPerOp, qcFormation7.AllocsPerOp))
	}

	if qcValidation7 != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>QC Validation (7 nodes)</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(qcValidation7.NsPerOp), FormatDuration(qcValidation7.NsPerOp),
			FormatOpsPerSec(qcValidation7.NsPerOp),
			qcValidation7.BytesPerOp, qcValidation7.AllocsPerOp))
	}

	if voteAggregation != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>Vote Aggregation</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(voteAggregation.NsPerOp), FormatDuration(voteAggregation.NsPerOp),
			FormatOpsPerSec(voteAggregation.NsPerOp),
			voteAggregation.BytesPerOp, voteAggregation.AllocsPerOp))
	}

	if qcFormationStandalone != nil {
		buf.WriteString(fmt.Sprintf(`<tr>
                <td><strong>QC Formation (standalone)</strong></td>
                <td style="text-align: right;" class="%s">%s</td>
                <td style="text-align: right;">%s</td>
                <td style="text-align: right;">%d B</td>
                <td style="text-align: right;">%d</td>
            </tr>`,
			ClassifySpeed(qcFormationStandalone.NsPerOp), FormatDuration(qcFormationStandalone.NsPerOp),
			FormatOpsPerSec(qcFormationStandalone.NsPerOp),
			qcFormationStandalone.BytesPerOp, qcFormationStandalone.AllocsPerOp))
	}

	buf.WriteString(`</tbody>
    </table>
</div>`)

	return buf.String()
}

func generatePerformanceSummary(results []BenchmarkResult) string {
	var buf bytes.Buffer

	buf.WriteString(`<div class="section">
    <h2>Performance Summary</h2>
    <div class="metrics-grid">`)

	// Key metrics with correct patterns
	metrics := []struct {
		name    string
		pattern string
		unit    string
	}{
		{"Ed25519 Sign", "Ed25519Sign-", "ops/sec"},
		{"Ed25519 Verify", "Ed25519Verify-", "ops/sec"},
		{"BLS Sign", "BLSSign-", "ops/sec"},
		{"BLS Verify", "BLSVerify-", "ops/sec"},
		{"BLS Aggregate", "BLSAggregate-", "ops/sec"},
		{"BLS Agg Verify", "BLSAggregateVerify-", "ops/sec"},
		{"Vote Creation", "VoteCreation-", "ops/sec"},
		{"Vote Verify", "VoteVerification-", "ops/sec"},
	}

	for _, m := range metrics {
		result := findBenchmark(results, m.pattern)
		if result != nil {
			speedClass := ClassifySpeed(result.NsPerOp)
			buf.WriteString(fmt.Sprintf(`<div class="metric-card">
            <div class="label">%s</div>
            <div class="value %s">%s</div>
            <div class="unit">%s</div>
            <div class="detail">%s per op</div>
        </div>`, m.name, speedClass, FormatOpsPerSec(result.NsPerOp), m.unit, FormatDuration(result.NsPerOp)))
		}
	}

	buf.WriteString(`</div></div>`)
	return buf.String()
}

func generateDetailedBenchmarks(results []BenchmarkResult) string {
	var buf bytes.Buffer

	buf.WriteString(`<div class="section">
    <h2>Detailed Benchmark Results</h2>
    <p style="color: var(--text-secondary); margin-bottom: 1.5rem;">
        Complete benchmark data including iterations, timing, memory usage, and allocations.
    </p>`)

	// Group benchmarks by category
	categories := map[string][]BenchmarkResult{
		"Cryptography":     {},
		"QC Operations":    {},
		"Vote Operations":  {},
		"Timer Operations": {},
		"Other":            {},
	}

	for _, r := range results {
		name := strings.ToLower(r.Name)
		switch {
		case strings.Contains(name, "ed25519") || strings.Contains(name, "bls"):
			categories["Cryptography"] = append(categories["Cryptography"], r)
		case strings.Contains(name, "qc"):
			categories["QC Operations"] = append(categories["QC Operations"], r)
		case strings.Contains(name, "vote"):
			categories["Vote Operations"] = append(categories["Vote Operations"], r)
		case strings.Contains(name, "timer"):
			categories["Timer Operations"] = append(categories["Timer Operations"], r)
		// Skip benchmarks that don't belong in the report (consensus library, not blockchain)
		case strings.Contains(name, "blockvalidation"):
			continue
		default:
			categories["Other"] = append(categories["Other"], r)
		}
	}

	// Sort categories for consistent output
	categoryOrder := []string{"Cryptography", "QC Operations", "Vote Operations", "Timer Operations", "Other"}

	for _, category := range categoryOrder {
		benchmarks := categories[category]
		if len(benchmarks) == 0 {
			continue
		}

		// Sort benchmarks by name within category
		sort.Slice(benchmarks, func(i, j int) bool {
			return benchmarks[i].Name < benchmarks[j].Name
		})

		badge := CategoryBadge(category)
		buf.WriteString(fmt.Sprintf(`<h3>%s %s</h3>
        <table>
            <thead>
                <tr>
                    <th>Benchmark</th>
                    <th style="text-align: right;">Iterations</th>
                    <th style="text-align: right;">Time</th>
                    <th style="text-align: right;">Ops/sec</th>
                    <th style="text-align: right;">Memory</th>
                    <th style="text-align: right;">Allocs</th>
                </tr>
            </thead>
            <tbody>`, badge, category))

		for _, b := range benchmarks {
			speedClass := ClassifySpeed(b.NsPerOp)
			// Clean up benchmark name for display
			displayName := strings.TrimPrefix(b.Name, "Benchmark")
			buf.WriteString(fmt.Sprintf(`<tr>
                    <td class="benchmark-name">%s</td>
                    <td style="text-align: right;">%d</td>
                    <td style="text-align: right;" class="%s">%s</td>
                    <td style="text-align: right;">%s</td>
                    <td style="text-align: right;">%d B</td>
                    <td style="text-align: right;">%d</td>
                </tr>`,
				displayName, b.Iterations, speedClass, FormatDuration(b.NsPerOp),
				FormatOpsPerSec(b.NsPerOp), b.BytesPerOp, b.AllocsPerOp))
		}

		buf.WriteString(`</tbody></table>`)
	}

	buf.WriteString(`</div>`)
	return buf.String()
}

func findBenchmark(results []BenchmarkResult, pattern string) *BenchmarkResult {
	for i := range results {
		if strings.Contains(results[i].Name, pattern) {
			return &results[i]
		}
	}
	return nil
}
