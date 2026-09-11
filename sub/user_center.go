package sub

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/util"
)

type UserCenterService struct {
	service.ClientService
	service.SettingService
	service.StatsService
}

type userCenterView struct {
	Name             string
	Enabled          bool
	Online           bool
	StateLabel       string
	OnlineLabel      string
	Used             string
	Upload           string
	Download         string
	LifetimeUpload   string
	LifetimeDownload string
	Quota            string
	Remaining        string
	UsagePercent     float64
	ExpiryAt         string
	ExpiryRemaining  string
	ExpiryExpired    bool
	ResetTitle       string
	ResetDetail      string
	UpdateInterval   string
	ProfileTitle     string
}

func (s *UserCenterService) Build(subID string, now time.Time) (userCenterView, []string, error) {
	client, err := s.ClientService.GetByName(subID)
	if err != nil {
		return userCenterView{}, nil, err
	}

	online, err := s.StatsService.IsUserOnline(client.Name)
	if err != nil {
		return userCenterView{}, nil, err
	}
	if !client.Enable {
		online = false
	}

	updateInterval, _ := s.SettingService.GetSubUpdates()
	return newUserCenterView(client, online, updateInterval, now), util.GetHeaders(client, updateInterval), nil
}

func newUserCenterView(client *model.Client, online bool, updateInterval int, now time.Time) userCenterView {
	up := nonNegative(client.Up)
	down := nonNegative(client.Down)
	used := safeTrafficSum(up, down)
	lifetimeUp := safeTrafficSum(nonNegative(client.TotalUp), up)
	lifetimeDown := safeTrafficSum(nonNegative(client.TotalDown), down)

	view := userCenterView{
		Name:             client.Name,
		Enabled:          client.Enable,
		Online:           client.Enable && online,
		StateLabel:       "已停用",
		OnlineLabel:      "离线",
		Used:             formatUserCenterBytes(used),
		Upload:           formatUserCenterBytes(up),
		Download:         formatUserCenterBytes(down),
		LifetimeUpload:   formatUserCenterBytes(lifetimeUp),
		LifetimeDownload: formatUserCenterBytes(lifetimeDown),
		ProfileTitle:     client.Name,
	}
	if view.Enabled {
		view.StateLabel = "已启用"
	}
	if view.Online {
		view.OnlineLabel = "在线"
	}

	if client.Volume > 0 {
		quota := nonNegative(client.Volume)
		remaining := quota - used
		if remaining < 0 {
			remaining = 0
		}
		view.Quota = formatUserCenterBytes(quota)
		view.Remaining = formatUserCenterBytes(remaining)
		view.UsagePercent = math.Min(100, float64(used)/float64(quota)*100)
	} else {
		view.Quota = "不限量"
		view.Remaining = "不限量"
	}

	if client.Expiry > 0 {
		expiry := time.Unix(client.Expiry, 0).In(now.Location())
		view.ExpiryAt = expiry.Format("2006-01-02 15:04 MST")
		remaining := expiry.Sub(now)
		if remaining <= 0 {
			view.ExpiryExpired = true
			view.ExpiryRemaining = "已到期"
		} else {
			view.ExpiryRemaining = "剩余 " + formatUserCenterDuration(remaining)
		}
	} else {
		view.ExpiryAt = "永久有效"
		view.ExpiryRemaining = "无到期时间"
	}

	view.ResetTitle, view.ResetDetail = describeClientReset(client, now)
	if updateInterval > 0 {
		view.UpdateInterval = fmt.Sprintf("每 %d 小时", updateInterval)
	} else {
		view.UpdateInterval = "由客户端决定"
	}
	return view
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func safeTrafficSum(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func formatUserCenterBytes(value int64) string {
	value = nonNegative(value)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func formatUserCenterDuration(value time.Duration) string {
	if value < time.Minute {
		return "不足 1 分钟"
	}
	days := int(value / (24 * time.Hour))
	value %= 24 * time.Hour
	hours := int(value / time.Hour)
	value %= time.Hour
	minutes := int(value / time.Minute)
	parts := make([]string, 0, 2)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d 天", days))
	}
	if hours > 0 && len(parts) < 2 {
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
	}
	if minutes > 0 && len(parts) < 2 {
		parts = append(parts, fmt.Sprintf("%d 分钟", minutes))
	}
	return strings.Join(parts, " ")
}

func describeClientReset(client *model.Client, now time.Time) (string, string) {
	if client.DelayStart {
		if client.AutoReset {
			return "等待首次使用", fmt.Sprintf("首次产生流量后开始计时，此后每 %d 天重置本周期流量", client.ResetDays)
		}
		return "等待首次使用", fmt.Sprintf("首次产生流量后开始 %d 天有效期", client.ResetDays)
	}
	if !client.AutoReset {
		return "未启用周期重置", "当前用量不会按周期自动清零"
	}
	if client.NextReset > 0 {
		next := time.Unix(client.NextReset, 0).In(now.Location())
		remaining := next.Sub(now)
		if remaining > 0 {
			return "每 " + fmt.Sprint(client.ResetDays) + " 天", next.Format("2006-01-02 15:04 MST") + " · 剩余 " + formatUserCenterDuration(remaining)
		}
		return "每 " + fmt.Sprint(client.ResetDays) + " 天", "重置任务等待执行"
	}
	return "每 " + fmt.Sprint(client.ResetDays) + " 天", "下一次重置时间尚未生成"
}

func renderUserCenter(writer io.Writer, view userCenterView) error {
	return userCenterTemplate.Execute(writer, view)
}

var userCenterTemplate = template.Must(template.New("user-center").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
  <meta name="color-scheme" content="light dark">
  <meta name="theme-color" media="(prefers-color-scheme: light)" content="#eef4ff">
  <meta name="theme-color" media="(prefers-color-scheme: dark)" content="#07101f">
  <title>{{.Name}} · NovaPanel</title>
  <style>
    :root{color-scheme:light dark;font:100%/1.5 -apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI",sans-serif;--bg:#edf4ff;--ink:#10213d;--muted:#5e6d83;--glass:rgba(255,255,255,.62);--glass-strong:rgba(255,255,255,.78);--line:rgba(78,104,145,.16);--accent:#377dff;--accent-2:#6f5cff;--good:#14936f;--quiet:#75849a;--danger:#d74d64;--shadow:0 28px 70px rgba(38,67,112,.15),inset 0 1px rgba(255,255,255,.74)}
    *{box-sizing:border-box}
    html{min-height:100%;background:var(--bg)}
    body{min-height:100%;margin:0;color:var(--ink);background:radial-gradient(circle at 14% 4%,rgba(69,145,255,.26),transparent 36rem),radial-gradient(circle at 91% 14%,rgba(139,105,255,.18),transparent 32rem),linear-gradient(155deg,#f5f9ff 0%,#e9f1ff 46%,#f6f1ff 100%);overflow-x:hidden}
    body:before{content:"";position:fixed;inset:0;pointer-events:none;opacity:.26;background-image:linear-gradient(rgba(255,255,255,.42) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.42) 1px,transparent 1px);background-size:42px 42px;mask-image:linear-gradient(to bottom,black,transparent 72%)}
    a{color:inherit}
    .shell{position:relative;width:min(70rem,100%);margin:0 auto;padding:max(1.25rem,env(safe-area-inset-top)) 1rem max(2.5rem,env(safe-area-inset-bottom))}
    .topbar{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin:.25rem .25rem 1.1rem}
    .brand{display:flex;align-items:center;gap:.7rem;font-weight:720;letter-spacing:-.02em}
    .mark{display:grid;place-items:center;width:2rem;height:2rem;border-radius:.65rem;color:white;background:linear-gradient(145deg,var(--accent),var(--accent-2));box-shadow:0 8px 22px rgba(64,100,255,.28),inset 0 1px rgba(255,255,255,.5)}
    .eyebrow{margin:0;color:var(--muted);font-size:.74rem;font-weight:700;letter-spacing:.11em;text-transform:uppercase}
    .glass{border:1px solid var(--line);background:var(--glass);box-shadow:var(--shadow);backdrop-filter:blur(28px) saturate(155%);-webkit-backdrop-filter:blur(28px) saturate(155%)}
    .hero{position:relative;overflow:hidden;border-radius:1.75rem;padding:clamp(1.35rem,4vw,2.4rem)}
    .hero:after{content:"";position:absolute;width:18rem;height:18rem;right:-8rem;top:-11rem;border-radius:50%;background:linear-gradient(145deg,rgba(67,141,255,.34),rgba(126,92,255,.16));filter:blur(2px);pointer-events:none}
    .hero-row{position:relative;z-index:1;display:flex;align-items:flex-start;justify-content:space-between;gap:1.25rem;flex-wrap:wrap}
    h1{max-width:44rem;margin:.28rem 0 .55rem;font-size:clamp(2rem,7vw,4.35rem);font-weight:750;line-height:1.02;letter-spacing:-.048em;overflow-wrap:anywhere}
    .subtitle{margin:0;color:var(--muted);font-size:clamp(.95rem,2vw,1.08rem)}
    .chips{display:flex;gap:.55rem;flex-wrap:wrap}
    .chip{display:inline-flex;align-items:center;gap:.43rem;min-height:2.15rem;padding:.42rem .78rem;border:1px solid var(--line);border-radius:999px;background:rgba(255,255,255,.42);font-size:.81rem;font-weight:680;white-space:nowrap}
    .dot{width:.47rem;height:.47rem;border-radius:50%;background:var(--quiet);box-shadow:0 0 0 .22rem color-mix(in srgb,var(--quiet) 14%,transparent)}
    .chip.good .dot{background:var(--good);box-shadow:0 0 0 .22rem color-mix(in srgb,var(--good) 14%,transparent)}
    .chip.bad .dot{background:var(--danger);box-shadow:0 0 0 .22rem color-mix(in srgb,var(--danger) 14%,transparent)}
    .usage{position:relative;z-index:1;margin-top:2rem}
    .usage-head{display:flex;align-items:end;justify-content:space-between;gap:1rem;margin-bottom:.7rem}
    .usage strong{font-size:clamp(1.55rem,4vw,2.2rem);letter-spacing:-.035em}
    .usage-copy{text-align:right;color:var(--muted);font-size:.82rem}
    progress{display:block;width:100%;height:.62rem;border:0;border-radius:999px;overflow:hidden;background:rgba(103,125,158,.14)}
    progress::-webkit-progress-bar{background:rgba(103,125,158,.14);border-radius:999px}
    progress::-webkit-progress-value{border-radius:999px;background:linear-gradient(90deg,var(--accent),var(--accent-2));box-shadow:0 0 18px rgba(73,111,255,.45)}
    progress::-moz-progress-bar{border-radius:999px;background:linear-gradient(90deg,var(--accent),var(--accent-2))}
    .metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.75rem;margin-top:.75rem}
    .metric,.card{border:1px solid var(--line);background:rgba(255,255,255,.42)}
    .metric{min-height:7rem;padding:1rem;border-radius:1.25rem}
    .label{display:block;color:var(--muted);font-size:.76rem;font-weight:650;letter-spacing:.02em}
    .value{display:block;margin-top:.35rem;font-size:clamp(1.1rem,3.6vw,1.45rem);font-weight:720;letter-spacing:-.025em;overflow-wrap:anywhere}
    .grid{display:grid;grid-template-columns:1fr;gap:.85rem;margin-top:.85rem}
    .card{border-radius:1.4rem;padding:1.15rem}
    .card h2{display:flex;align-items:center;gap:.6rem;margin:0 0 .95rem;font-size:1rem;letter-spacing:-.015em}
    .icon{display:grid;place-items:center;width:2rem;height:2rem;border-radius:.7rem;color:var(--accent);background:color-mix(in srgb,var(--accent) 11%,transparent)}
    .rows{display:grid;gap:.82rem}
    .row{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem;padding-bottom:.82rem;border-bottom:1px solid var(--line)}
    .row:last-child{padding:0;border:0}
    .row .value{margin:0;text-align:right;font-size:.95rem}
    .detail{display:block;margin-top:.18rem;color:var(--muted);font-size:.75rem;font-weight:500}.danger{color:var(--danger)}
    .actions{grid-column:1/-1}
    .action-list{display:grid;gap:.65rem}
    .action{display:flex;align-items:center;gap:.85rem;min-height:4.3rem;padding:.85rem .95rem;border:1px solid var(--line);border-radius:1rem;text-decoration:none;background:rgba(255,255,255,.4);transition:transform .18s ease,background-color .18s ease,border-color .18s ease}
    .action:hover{background:rgba(255,255,255,.7);border-color:color-mix(in srgb,var(--accent) 35%,var(--line));transform:translateY(-1px)}
    .action:active{transform:scale(.985);transition-duration:.08s}
    .action:focus-visible{outline:3px solid color-mix(in srgb,var(--accent) 34%,transparent);outline-offset:2px}
    .action-copy{min-width:0;flex:1}
    .action-title{display:block;font-size:.9rem;font-weight:690}
    .action-url{display:block;overflow:hidden;margin-top:.12rem;color:var(--muted);font:500 .72rem/1.35 ui-monospace,SFMono-Regular,Menlo,monospace;text-overflow:ellipsis;white-space:nowrap}
    .arrow{color:var(--muted);font-size:1.15rem}
    .import{margin-top:.7rem;color:white;border-color:transparent;background:linear-gradient(135deg,var(--accent),var(--accent-2));box-shadow:0 12px 28px rgba(64,93,235,.22)}
    .import:hover{background:linear-gradient(135deg,#2d74f5,#6751ee);border-color:transparent}
    .import .icon{color:white;background:rgba(255,255,255,.16)}
    .import .action-url,.import .arrow{color:rgba(255,255,255,.78)}
    .foot{margin:1.25rem .3rem 0;color:var(--muted);font-size:.72rem;text-align:center}
    .toast{position:fixed;z-index:10;left:50%;bottom:max(1.25rem,env(safe-area-inset-bottom));padding:.7rem 1rem;border:1px solid var(--line);border-radius:999px;color:var(--ink);background:var(--glass-strong);box-shadow:var(--shadow);backdrop-filter:blur(22px);transform:translate(-50%,1rem);opacity:0;pointer-events:none;transition:opacity .2s ease,transform .3s cubic-bezier(.2,.75,.2,1)}
    .toast.show{opacity:1;transform:translate(-50%,0)}
    @media(min-width:46rem){.shell{padding-left:1.5rem;padding-right:1.5rem}.metrics{grid-template-columns:repeat(4,minmax(0,1fr))}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}.action-list{grid-template-columns:repeat(3,minmax(0,1fr))}.action{align-items:flex-start;min-height:7.5rem;flex-wrap:wrap}.action-copy{flex-basis:calc(100% - 3rem)}.import{min-height:4.3rem;align-items:center;flex-wrap:nowrap}.import .action-copy{flex-basis:auto}}
    @media(prefers-color-scheme:dark){:root{--bg:#07101f;--ink:#edf5ff;--muted:#9aaac0;--glass:rgba(11,24,45,.64);--glass-strong:rgba(14,28,50,.9);--line:rgba(183,207,240,.14);--accent:#64a2ff;--accent-2:#9a87ff;--good:#43d6a4;--quiet:#7d8ea7;--danger:#ff7188;--shadow:0 30px 80px rgba(0,0,0,.34),inset 0 1px rgba(255,255,255,.09)}body{background:radial-gradient(circle at 12% 0,rgba(30,102,207,.34),transparent 34rem),radial-gradient(circle at 92% 10%,rgba(94,57,184,.24),transparent 31rem),linear-gradient(155deg,#08111f,#0a1629 48%,#101126)}.metric,.card,.action{background:rgba(12,27,49,.48)}.chip{background:rgba(15,31,54,.5)}.action:hover{background:rgba(23,44,73,.7)}}
    @media(prefers-reduced-motion:reduce){*,*:before,*:after{scroll-behavior:auto!important;transition-duration:.01ms!important;animation-duration:.01ms!important}.action:hover{transform:none}}
    @media(prefers-reduced-transparency:reduce){.glass,.toast{background:var(--glass-strong);backdrop-filter:none;-webkit-backdrop-filter:none}}
    @media(prefers-contrast:more){:root{--line:currentColor}.glass,.metric,.card,.action,.chip{background:var(--bg)}}
  </style>
</head>
<body>
  <main class="shell">
    <header class="topbar"><div class="brand"><span class="mark" aria-hidden="true">N</span><span>NovaPanel</span></div><p class="eyebrow">Subscription Center</p></header>
    <section class="hero glass" aria-labelledby="profile-title">
      <div class="hero-row">
        <div><p class="eyebrow">你的订阅</p><h1 id="profile-title" data-testid="client-name">{{.Name}}</h1><p class="subtitle">查看状态、用量与客户端配置入口</p></div>
        <div class="chips" aria-label="订阅状态">
          <span class="chip {{if .Enabled}}good{{else}}bad{{end}}" data-testid="enabled-state"><span class="dot"></span>{{.StateLabel}}</span>
          <span class="chip {{if .Online}}good{{end}}" data-testid="online-state"><span class="dot"></span>{{.OnlineLabel}}</span>
        </div>
      </div>
      <div class="usage">
        <div class="usage-head"><div><span class="label">本周期已用</span><strong data-testid="used-traffic">{{.Used}}</strong></div><div class="usage-copy">剩余 {{.Remaining}}<br>配额 {{.Quota}}</div></div>
        <progress value="{{.UsagePercent}}" max="100" aria-label="流量使用比例">{{.UsagePercent}}%</progress>
      </div>
    </section>

    <section class="metrics" aria-label="流量详情">
      <article class="metric"><span class="label">本周期上传</span><span class="value" data-testid="upload-traffic">{{.Upload}}</span></article>
      <article class="metric"><span class="label">本周期下载</span><span class="value" data-testid="download-traffic">{{.Download}}</span></article>
      <article class="metric"><span class="label">上传总量</span><span class="value" data-testid="lifetime-upload">{{.LifetimeUpload}}</span></article>
      <article class="metric"><span class="label">下载总量</span><span class="value" data-testid="lifetime-download">{{.LifetimeDownload}}</span></article>
    </section>

    <section class="grid">
      <article class="card">
        <h2><span class="icon" aria-hidden="true">◷</span>有效期与重置</h2>
        <div class="rows">
          <div class="row"><span class="label">有效期</span><span class="value {{if .ExpiryExpired}}danger{{end}}" data-testid="expiry">{{.ExpiryAt}}<small class="detail">{{.ExpiryRemaining}}</small></span></div>
          <div class="row"><span class="label">流量重置</span><span class="value" data-testid="reset-info">{{.ResetTitle}}<small class="detail">{{.ResetDetail}}</small></span></div>
        </div>
      </article>
      <article class="card">
        <h2><span class="icon" aria-hidden="true">↻</span>订阅资料</h2>
        <div class="rows">
          <div class="row"><span class="label">配置名称</span><span class="value" data-testid="profile-title">{{.ProfileTitle}}</span></div>
          <div class="row"><span class="label">建议更新</span><span class="value" data-testid="update-interval">{{.UpdateInterval}}<small class="detail">客户端会按此间隔刷新配置</small></span></div>
        </div>
      </article>
      <article class="card actions">
        <h2><span class="icon" aria-hidden="true">⌁</span>添加到客户端</h2>
        <div class="action-list">
          <a class="action" href="?format=raw" data-copy-format="raw"><span class="icon" aria-hidden="true">⧉</span><span class="action-copy"><span class="action-title">复制通用订阅</span><span class="action-url">?format=raw</span></span><span class="arrow" aria-hidden="true">›</span></a>
          <a class="action" href="?format=clash" data-copy-format="clash"><span class="icon" aria-hidden="true">⧉</span><span class="action-copy"><span class="action-title">复制 Clash / Mihomo</span><span class="action-url">?format=clash</span></span><span class="arrow" aria-hidden="true">›</span></a>
          <a class="action" href="?format=json" data-copy-format="json"><span class="icon" aria-hidden="true">⧉</span><span class="action-copy"><span class="action-title">复制 sing-box JSON</span><span class="action-url">?format=json</span></span><span class="arrow" aria-hidden="true">›</span></a>
        </div>
        <a class="action import" id="clash-import" href="?format=clash"><span class="icon" aria-hidden="true">↓</span><span class="action-copy"><span class="action-title">导入 Clash / Mihomo</span><span class="action-url">使用标准 clash://install-config 深链；无法唤起时请复制上方链接</span></span><span class="arrow" aria-hidden="true">›</span></a>
      </article>
    </section>
    <p class="foot">此页面只展示当前订阅标识可访问的信息 · NovaPanel</p>
  </main>
  <div class="toast" id="toast" role="status" aria-live="polite"></div>
  <script>
    (() => {
      const subscriptionURL = (format) => {
        const url = new URL(window.location.href);
        url.hash = "";
        url.searchParams.set("format", format);
        return url.toString();
      };
      document.querySelectorAll("[data-copy-format]").forEach((item) => {
        const format = item.dataset.copyFormat;
        const url = subscriptionURL(format);
        item.querySelector(".action-url").textContent = url;
        item.addEventListener("click", async (event) => {
          event.preventDefault();
          try {
            if (!navigator.clipboard || !window.isSecureContext) throw new Error("clipboard unavailable");
            await navigator.clipboard.writeText(url);
            const toast = document.getElementById("toast");
            toast.textContent = "订阅链接已复制";
            toast.classList.add("show");
            window.setTimeout(() => toast.classList.remove("show"), 1800);
          } catch (_) {
            window.prompt("复制此订阅链接", url);
          }
        });
      });
      const clashURL = subscriptionURL("clash");
      document.getElementById("clash-import").href = "clash://install-config?url=" + encodeURIComponent(clashURL);
    })();
  </script>
</body>
</html>`))
