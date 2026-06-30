package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"
)

// ─── Data structures (mirror main package) ────────────────────────────────────

type NodeStat struct {
	IP    string `json:"IP"`
	Bytes int64  `json:"Bytes"`
}

type ProbeResult struct {
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	PeakMbps   float64    `json:"peak_mbps,omitempty"`
	AvgMbps    float64    `json:"avg_mbps,omitempty"`
	UploadMbps float64    `json:"upload_mbps,omitempty"`
	TopIPs     []NodeStat `json:"top_ips,omitempty"`
	ServerName string     `json:"server_name,omitempty"`
	Sponsor    string     `json:"sponsor,omitempty"`
	ServerIP   string     `json:"server_ip,omitempty"`
	ClientIP   string     `json:"client_ip,omitempty"`
}

type ConnectionInfo struct {
	Status            string  `json:"status"`
	TimeToConnectSecs float64 `json:"time_to_connect_seconds,omitempty"`
	AssignedIP        string  `json:"assigned_ip,omitempty"`
	Error             string  `json:"error,omitempty"`
}

type PackageReport struct {
	PackageName         string                 `json:"package_name"`
	InterfaceName       string                 `json:"interface_name"`
	Order               int                    `json:"order"`
	AdvertisedSpeedMbps int                    `json:"advertised_speed_mbps"`
	Connection          ConnectionInfo         `json:"connection"`
	Tests               map[string]ProbeResult `json:"tests"`
}

type MasterReport struct {
	Timestamp            string          `json:"timestamp"`
	RouterIdentity       string          `json:"router_identity"`
	TotalDurationSeconds float64         `json:"total_duration_seconds"`
	Packages             []PackageReport `json:"packages"`
}

// ─── Template helpers ─────────────────────────────────────────────────────────

type TemplateData struct {
	Report      MasterReport
	GeneratedAt string
	LogoBase64  string
	ProbeNames  []string
}

func (td TemplateData) ProbeLabel(key string) string {
	labels := map[string]string{
		"youtube":   "YouTube",
		"facebook":  "Facebook",
		"fast":      "Fast.com",
		"download":  "Direct Download",
		"speedtest": "Speedtest CLI",
	}
	if l, ok := labels[key]; ok {
		return l
	}
	return strings.Title(key)
}

func (td TemplateData) StatusClass(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCESS", "CONNECTED":
		return "success"
	case "FAILED":
		return "failed"
	case "SKIPPED":
		return "skipped"
	default:
		return "unknown"
	}
}

func (td TemplateData) StatusIcon(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCESS", "CONNECTED":
		return "✓"
	case "FAILED":
		return "✗"
	case "SKIPPED":
		return "—"
	default:
		return "?"
	}
}

func (td TemplateData) FormatDuration(secs float64) string {
	d := time.Duration(secs) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func (td TemplateData) SpeedBarWidth(mbps float64, advertised int) float64 {
	if advertised <= 0 {
		return 0
	}
	pct := (mbps / float64(advertised)) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

func (td TemplateData) FormatBytes(b int64) string {
	const mb = 1024 * 1024
	const kb = 1024
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (td TemplateData) SuccessCount() int {
	count := 0
	for _, p := range td.Report.Packages {
		if strings.ToUpper(p.Connection.Status) == "CONNECTED" {
			count++
		}
	}
	return count
}

func (td TemplateData) AvgPeakSpeed() float64 {
	var total float64
	var count int
	for _, p := range td.Report.Packages {
		if strings.ToUpper(p.Connection.Status) != "CONNECTED" {
			continue
		}
		for _, r := range p.Tests {
			if strings.ToUpper(r.Status) == "SUCCESS" && r.PeakMbps > 0 {
				total += r.PeakMbps
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	resultsFile := flag.String("results", "results.json", "Path to results.json")
	outputFile := flag.String("output", "report.html", "Path to output HTML file")
	logoFile := flag.String("logo", "logo.png", "Path to logo image (optional)")
	flag.Parse()

	// Load results
	data, err := os.ReadFile(*resultsFile)
	if err != nil {
		fatalf("Cannot read results file %q: %v", *resultsFile, err)
	}

	var report MasterReport
	if err := json.Unmarshal(data, &report); err != nil {
		fatalf("Cannot parse results JSON: %v", err)
	}

	// Encode logo
	var logoB64 string
	if logoData, err := os.ReadFile(*logoFile); err == nil {
		ext := strings.ToLower(*logoFile)
		mime := "image/png"
		if strings.HasSuffix(ext, ".jpg") || strings.HasSuffix(ext, ".jpeg") {
			mime = "image/jpeg"
		} else if strings.HasSuffix(ext, ".svg") {
			mime = "image/svg+xml"
		}
		logoB64 = fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(logoData))
		fmt.Printf("[INFO] Logo loaded from %q\n", *logoFile)
	} else {
		fmt.Printf("[INFO] Logo file not found (%v) — will use text header.\n", err)
	}

	// Determine probe order
	probeOrder := []string{"youtube", "facebook", "fast", "download", "speedtest"}
	var activeProbes []string
	for _, name := range probeOrder {
		for _, pkg := range report.Packages {
			if _, ok := pkg.Tests[name]; ok {
				activeProbes = append(activeProbes, name)
				break
			}
		}
	}

	td := TemplateData{
		Report:      report,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		LogoBase64:  logoB64,
		ProbeNames:  activeProbes,
	}

	// Parse and execute template
	funcMap := template.FuncMap{
		"upper": strings.ToUpper,
		"add":   func(a, b int) int { return a + b },
	}
	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlTemplate)
	if err != nil {
		fatalf("Template parse error: %v", err)
	}

	out, err := os.Create(*outputFile)
	if err != nil {
		fatalf("Cannot create output file %q: %v", *outputFile, err)
	}
	defer out.Close()

	if err := tmpl.Execute(out, td); err != nil {
		fatalf("Template execution error: %v", err)
	}

	fmt.Printf("[INFO] Report written to %q\n", *outputFile)
}

func fatalf(format string, args ...any) {
	fmt.Printf("[FATAL] "+format+"\n", args...)
	os.Exit(1)
}

// ─── HTML Template ────────────────────────────────────────────────────────────

const htmlTemplate = `<!DOCTYPE html>
<html lang="ar" dir="rtl">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>تقرير قياس السرعة — {{.Report.RouterIdentity}}</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Cairo:wght@300;400;600;700;800&display=swap');

  :root {
    --purple:      #5B2D8E;
    --purple-dark: #3D1A6E;
    --purple-light:#7B4DB5;
    --orange:      #F97316;
    --orange-light:#FDB97D;
    --success:     #22c55e;
    --failed:      #ef4444;
    --skipped:     #94a3b8;
    --bg:          #F8F7FC;
    --card:        #FFFFFF;
    --border:      #E5E0F0;
    --text:        #1E1433;
    --muted:       #6B7280;
  }

  * { box-sizing: border-box; margin: 0; padding: 0; }

  body {
    font-family: 'Cairo', 'Segoe UI', sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 14px;
  }

  /* ── Header ── */
  .header {
    background: linear-gradient(135deg, var(--purple-dark) 0%, var(--purple) 60%, var(--purple-light) 100%);
    color: #fff;
    padding: 32px 48px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
  }
  .header-logo img  { height: 56px; filter: brightness(0) invert(1); }
  .header-logo span { font-size: 28px; font-weight: 800; letter-spacing: 1px; }
  .header-info      { text-align: left; }
  .header-info h1   { font-size: 22px; font-weight: 700; margin-bottom: 4px; }
  .header-info p    { font-size: 13px; opacity: 0.8; }
  .header-badge {
    background: var(--orange);
    color: #fff;
    font-size: 12px;
    font-weight: 700;
    padding: 4px 14px;
    border-radius: 20px;
    margin-top: 8px;
    display: inline-block;
  }

  /* ── Summary cards ── */
  .summary {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    padding: 24px 48px;
  }
  .card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px 24px;
    box-shadow: 0 1px 4px rgba(91,45,142,.06);
  }
  .card-label { font-size: 12px; color: var(--muted); font-weight: 600; text-transform: uppercase; letter-spacing: .5px; }
  .card-value { font-size: 28px; font-weight: 800; color: var(--purple); margin: 6px 0 2px; }
  .card-sub   { font-size: 12px; color: var(--muted); }
  .card.orange .card-value { color: var(--orange); }
  .card.green  .card-value { color: var(--success); }

  /* ── Section title ── */
  .section-title {
    padding: 0 48px 12px;
    font-size: 16px;
    font-weight: 700;
    color: var(--purple);
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .section-title::after {
    content: '';
    flex: 1;
    height: 2px;
    background: linear-gradient(to left, transparent, var(--border));
  }

  /* ── Package block ── */
  .packages { padding: 0 48px 32px; display: flex; flex-direction: column; gap: 24px; }

  .pkg-card {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 16px;
    overflow: hidden;
    box-shadow: 0 2px 8px rgba(91,45,142,.07);
    page-break-inside: avoid;
  }

  /* Package header */
  .pkg-head {
    background: linear-gradient(90deg, var(--purple) 0%, var(--purple-light) 100%);
    color: #fff;
    padding: 16px 24px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }
  .pkg-head-left   { display: flex; align-items: center; gap: 14px; }
  .pkg-number {
    background: rgba(255,255,255,.2);
    border-radius: 50%;
    width: 36px; height: 36px;
    display: flex; align-items: center; justify-content: center;
    font-weight: 800; font-size: 16px; flex-shrink: 0;
  }
  .pkg-name  { font-size: 18px; font-weight: 700; }
  .pkg-iface { font-size: 12px; opacity: .75; margin-top: 2px; font-family: monospace; }
  .pkg-speed-badge {
    background: var(--orange);
    color: #fff;
    font-weight: 800;
    font-size: 13px;
    padding: 6px 16px;
    border-radius: 20px;
    white-space: nowrap;
  }

  /* Connection info strip */
  .conn-strip {
    padding: 10px 24px;
    font-size: 12px;
    display: flex;
    align-items: center;
    gap: 20px;
    border-bottom: 1px solid var(--border);
  }
  .conn-strip.success { background: #f0fdf4; }
  .conn-strip.failed  { background: #fef2f2; }
  .conn-strip.skipped { background: #f8fafc; }
  .conn-dot {
    width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0;
  }
  .conn-dot.success { background: var(--success); }
  .conn-dot.failed  { background: var(--failed); }
  .conn-dot.skipped { background: var(--skipped); }
  .conn-label { font-weight: 700; }
  .conn-details { color: var(--muted); }
  .conn-error   { color: var(--failed); font-size: 11px; flex: 1; }

  /* Tests grid */
  .tests-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 1px;
    background: var(--border);
  }
  .test-cell {
    background: var(--card);
    padding: 16px 20px;
  }
  .test-source {
    font-size: 11px;
    font-weight: 700;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: .5px;
    margin-bottom: 8px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .test-status-dot {
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--skipped);
  }
  .test-status-dot.success { background: var(--success); }
  .test-status-dot.failed  { background: var(--failed); }

  .test-peak {
    font-size: 22px;
    font-weight: 800;
    color: var(--purple);
    line-height: 1;
  }
  .test-peak.failed  { color: var(--failed);  font-size: 14px; }
  .test-peak.skipped { color: var(--skipped); font-size: 14px; }
  .test-unit { font-size: 11px; color: var(--muted); font-weight: 400; }
  .test-avg  { font-size: 12px; color: var(--muted); margin-top: 4px; }

  /* Speed bar */
  .speed-bar-bg {
    height: 5px;
    background: var(--border);
    border-radius: 3px;
    margin-top: 8px;
    overflow: hidden;
  }
  .speed-bar-fill {
    height: 100%;
    border-radius: 3px;
    background: linear-gradient(90deg, var(--orange), var(--purple-light));
    transition: width .3s;
  }

  /* IPs */
  .top-ips { margin-top: 8px; }
  .ip-row  { display: flex; justify-content: space-between; font-size: 10px; color: var(--muted); font-family: monospace; }

  /* ── Footer ── */
  .footer {
    background: var(--purple-dark);
    color: rgba(255,255,255,.6);
    text-align: center;
    padding: 16px 48px;
    font-size: 12px;
  }
  .footer strong { color: #fff; }

  /* ── Print ── */
  @media print {
    body { background: #fff; }
    .header { background: var(--purple) !important; -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .pkg-head { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .speed-bar-fill { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .footer { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  }
</style>
</head>
<body>

<!-- ══ HEADER ═══════════════════════════════════════════════════════════ -->
<div class="header">
  <div class="header-logo">
    {{if .LogoBase64}}
      <img src="{{.LogoBase64}}" alt="FiberX Logo">
    {{else}}
      <span>FiberX</span>
    {{end}}
  </div>
  <div style="flex:1"></div>
  <div class="header-info">
    <h1>تقرير قياس جودة الباقات</h1>
    <p>الراوتر: <strong>{{.Report.RouterIdentity}}</strong></p>
    <p>{{.Report.Timestamp}}</p>
    <span class="header-badge">المدة الكلية: {{.FormatDuration .Report.TotalDurationSeconds}}</span>
  </div>
</div>

<!-- ══ SUMMARY ════════════════════════════════════════════════════════════ -->
<div class="summary">
  <div class="card">
    <div class="card-label">إجمالي الباقات</div>
    <div class="card-value">{{len .Report.Packages}}</div>
    <div class="card-sub">باقة مفحوصة</div>
  </div>
  <div class="card green">
    <div class="card-label">نجحت الاتصال</div>
    <div class="card-value">{{.SuccessCount}}</div>
    <div class="card-sub">من أصل {{len .Report.Packages}} باقة</div>
  </div>
  <div class="card orange">
    <div class="card-label">متوسط الذروة الكلي</div>
    <div class="card-value">{{printf "%.1f" .AvgPeakSpeed}}</div>
    <div class="card-sub">Mbps عبر كل الاختبارات</div>
  </div>
  <div class="card">
    <div class="card-label">مدة الفحص</div>
    <div class="card-value" style="font-size:20px">{{.FormatDuration .Report.TotalDurationSeconds}}</div>
    <div class="card-sub">وقت التشغيل الفعلي</div>
  </div>
</div>

<!-- ══ PACKAGES ═══════════════════════════════════════════════════════════ -->
<div class="section-title">تفاصيل كل باقة</div>

<div class="packages">
{{range $i, $pkg := .Report.Packages}}
{{$connClass := $.StatusClass $pkg.Connection.Status}}
<div class="pkg-card">

  <!-- Package header -->
  <div class="pkg-head">
    <div class="pkg-head-left">
      <div class="pkg-number">{{add $i 1}}</div>
      <div>
        <div class="pkg-name">{{$pkg.PackageName}}</div>
        <div class="pkg-iface">{{$pkg.InterfaceName}}</div>
      </div>
    </div>
    <div class="pkg-speed-badge">{{$pkg.AdvertisedSpeedMbps}} Mbps</div>
  </div>

  <!-- Connection strip -->
  <div class="conn-strip {{$connClass}}">
    <div class="conn-dot {{$connClass}}"></div>
    <div class="conn-label">
      {{if eq (upper $pkg.Connection.Status) "CONNECTED"}}متصل{{else if eq (upper $pkg.Connection.Status) "FAILED"}}فشل الاتصال{{else}}—{{end}}
    </div>
    {{if $pkg.Connection.AssignedIP}}
      <div class="conn-details">IP: {{$pkg.Connection.AssignedIP}}</div>
    {{end}}
    {{if $pkg.Connection.TimeToConnectSecs}}
      <div class="conn-details">زمن الاتصال: {{printf "%.1f" $pkg.Connection.TimeToConnectSecs}}s</div>
    {{end}}
    {{if $pkg.Connection.Error}}
      <div class="conn-error">{{$pkg.Connection.Error}}</div>
    {{end}}
  </div>

  <!-- Tests -->
  <div class="tests-grid">
    {{range $probeName := $.ProbeNames}}
    {{$result := index $pkg.Tests $probeName}}
    {{$rClass := $.StatusClass $result.Status}}
    <div class="test-cell">
      <div class="test-source">
        <div class="test-status-dot {{$rClass}}"></div>
        {{$.ProbeLabel $probeName}}
      </div>

      {{if eq (upper $result.Status) "SUCCESS"}}
        <div class="test-peak">{{printf "%.1f" $result.PeakMbps}}<span class="test-unit"> Mbps ذروة</span></div>
        <div class="test-avg">متوسط: {{printf "%.1f" $result.AvgMbps}} Mbps</div>
        {{if $result.UploadMbps}}
          <div class="test-avg">رفع: {{printf "%.1f" $result.UploadMbps}} Mbps</div>
        {{end}}
        <div class="speed-bar-bg">
          <div class="speed-bar-fill" style="width:{{printf "%.0f" ($.SpeedBarWidth $result.PeakMbps $pkg.AdvertisedSpeedMbps)}}%"></div>
        </div>
        {{if $result.TopIPs}}
          <div class="top-ips">
            {{range $result.TopIPs}}
              <div class="ip-row"><span>{{.IP}}</span><span>{{$.FormatBytes .Bytes}}</span></div>
            {{end}}
          </div>
        {{end}}
        {{if $result.ServerName}}
          <div class="test-avg" style="margin-top:4px">السيرفر: {{$result.ServerName}}</div>
        {{end}}
      {{else if eq (upper $result.Status) "FAILED"}}
        <div class="test-peak failed">فشل</div>
        {{if $result.Error}}
          <div class="conn-error" style="font-size:10px;margin-top:4px">{{$result.Error}}</div>
        {{end}}
      {{else}}
        <div class="test-peak skipped">تم التخطي</div>
        {{if $result.Error}}
          <div class="conn-error" style="font-size:10px;margin-top:4px">{{$result.Error}}</div>
        {{end}}
      {{end}}
    </div>
    {{end}}
  </div>

</div>
{{end}}
</div>

<!-- ══ FOOTER ════════════════════════════════════════════════════════════ -->
<div class="footer">
  تم إنشاء هذا التقرير بواسطة <strong>NetScalpel</strong> &nbsp;|&nbsp;
  {{.GeneratedAt}} &nbsp;|&nbsp;
  الراوتر: <strong>{{.Report.RouterIdentity}}</strong>
</div>

</body>
</html>`
