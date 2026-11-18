// Package django 是 Lemon Paperplane 的 Django 插件：识别 manage.py 与设置模块，用 runserver 运行，
// 提示未执行的迁移并提供「执行 migrate」。解释器与虚拟环境由 python 插件负责（服务 resolveEnv）。
package django

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	sdk "github.com/lemon-paperplane/lemon-paperplane-main/lemon-paperplane-main-plugin-sdk"
)

const defaultPort = 8000

// Plugin 实现 Detector、Analyzer、RunPreparer、Diagnoser、FixApplier、ActionInvoker。
type Plugin struct{}

// pluginID 是本插件 id。
const pluginID = "django"

// New 创建插件实例。
func New() any { return &Plugin{} }

var (
	settingsRe   = regexp.MustCompile(`DJANGO_SETTINGS_MODULE["']\s*,\s*["']([\w.]+)["']`)
	djangoMgmtRe = regexp.MustCompile(`django|DJANGO_SETTINGS_MODULE|execute_from_command_line`)
)

// findManagePy 在模块目录及其下两层找 manage.py（常见：根目录、src/、backend/、<project>/）。
func findManagePy(dir string, files []string) string {
	for _, f := range files {
		if f == "manage.py" {
			if djangoMgmtRe.MatchString(sdk.ReadText(filepath.Join(dir, "manage.py"), 64<<10)) {
				return "manage.py"
			}
		}
	}
	var found []string
	var walk func(d, rel string, depth int)
	walk = func(d, rel string, depth int) {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if !e.IsDir() || sdk.DefaultSkipDirs[e.Name()] || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
				continue
			}
			sub := filepath.Join(d, e.Name())
			r := e.Name()
			if rel != "" {
				r = rel + "/" + e.Name()
			}
			if sdk.FileExists(filepath.Join(sub, "pyvenv.cfg")) {
				continue
			}
			if mp := filepath.Join(sub, "manage.py"); sdk.FileExists(mp) && djangoMgmtRe.MatchString(sdk.ReadText(mp, 64<<10)) {
				found = append(found, r+"/manage.py")
			}
			if depth < 2 {
				walk(sub, r, depth+1)
			}
		}
	}
	walk(dir, "", 1)
	sort.Slice(found, func(i, j int) bool { return len(found[i]) < len(found[j]) })
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

// Detect：有 Django 的 manage.py（本目录或下两层）。
func (p *Plugin) Detect(ctx *sdk.Context, d *sdk.DetectParams) (*sdk.DetectResult, error) {
	deps := pythonFacetDeps(d.Facets)
	manage := ""
	if sdk.HasFile(d.Files, "manage.py") || deps["django"] {
		manage = findManagePy(d.Dir, d.Files)
	}
	if manage == "" {
		return &sdk.DetectResult{}, nil
	}
	data := map[string]any{"category": "framework", "manage": manage, "scripts": []any{manage}}
	if m := settingsRe.FindStringSubmatch(sdk.ReadText(filepath.Join(d.Dir, manage), 64<<10)); m != nil {
		data["settings"] = m[1]
	}
	return &sdk.DetectResult{Facets: []sdk.Facet{{ID: "django", Data: data}}}, nil
}

// Model 是 django 插件模型。
type Model struct {
	Manage   string   `json:"manage"`             // manage.py 相对模块目录
	Settings string   `json:"settings,omitempty"` // manage.py 里的默认 DJANGO_SETTINGS_MODULE
	Variants []string `json:"variants,omitempty"` // 可选的设置模块（settings/ 包里的 dev、prod…）
	Version  string   `json:"version,omitempty"`
}

func analyzeModule(dir string, facet *sdk.Facet) *Model {
	m := &Model{}
	if facet != nil && facet.Data != nil {
		m.Manage, _ = facet.Data["manage"].(string)
	}
	if m.Manage == "" {
		m.Manage = findManagePy(dir, nil)
	}
	if m.Manage == "" {
		return m
	}
	base := filepath.Join(dir, filepath.Dir(m.Manage))
	if s := settingsRe.FindStringSubmatch(sdk.ReadText(filepath.Join(dir, m.Manage), 64<<10)); s != nil {
		m.Settings = s[1]
	}
	if m.Settings != "" {
		m.Variants = append(m.Variants, m.Settings)
		// settings 是包（settings/__init__.py + dev.py/prod.py…）时列出各变体
		pkg := strings.TrimSuffix(m.Settings, ".settings")
		parts := strings.Split(m.Settings, ".")
		pkgDir := filepath.Join(append([]string{base}, parts...)...)
		if !sdk.DirExists(pkgDir) {
			pkgDir = filepath.Join(append([]string{base}, parts[:len(parts)-1]...)...)
			pkg = strings.Join(parts[:len(parts)-1], ".")
		} else {
			pkg = m.Settings
		}
		if sdk.DirExists(pkgDir) && sdk.FileExists(filepath.Join(pkgDir, "__init__.py")) {
			ents, _ := os.ReadDir(pkgDir)
			for _, e := range ents {
				n := e.Name()
				if strings.HasSuffix(n, ".py") && n != "__init__.py" && !strings.HasPrefix(n, "_") && (strings.Contains(pkgDir, "settings") || strings.Contains(n, "settings")) {
					v := pkg + "." + strings.TrimSuffix(n, ".py")
					if v != m.Settings && !strings.HasSuffix(v, ".urls") && !strings.HasSuffix(v, ".wsgi") && !strings.HasSuffix(v, ".asgi") {
						m.Variants = append(m.Variants, v)
					}
				}
			}
		}
	}
	return m
}

// Analyze 生成 runserver 运行配置。
func (p *Plugin) Analyze(ctx *sdk.Context, in *sdk.AnalyzeParams) (*sdk.AnalyzeResult, error) {
	if !in.Module.HasFacet("django") {
		return &sdk.AnalyzeResult{}, nil // 插件因 python facet 被拉起，但本模块不是 django 工程
	}
	m := analyzeModule(in.Module.Dir, in.Module.Facet("django"))
	var pm pythonModel
	in.Module.Model("python", &pm)
	m.Version = installedVersion(pm.Venv, "django")
	res := &sdk.AnalyzeResult{}
	res.Model, _ = json.Marshal(m)
	if m.Version != "" {
		res.Info = append(res.Info, sdk.InfoItem{Label: "Django", Value: shortVersion(m.Version), Icon: "icons/django.svg"})
	} else {
		res.Info = append(res.Info, sdk.InfoItem{Label: ctx.T("info.framework"), Value: "Django", Icon: "icons/django.svg"})
	}
	opts := []sdk.Option{} // 没有选项也输出 []，不能是 null（界面按数组解析）
	for _, v := range m.Variants {
		opts = append(opts, sdk.Option{Value: v, Label: v})
	}
	res.OptionValues = map[string][]sdk.Option{"django.settings": opts}
	if m.Manage == "" {
		return res, nil
	}
	cfg := sdk.RunConfiguration{
		ID:       sdk.StableID("django-server", m.Manage),
		Name:     "runserver",
		Type:     "django-server",
		Options:  map[string]any{"host": "127.0.0.1", "port": defaultPort, "manage": m.Manage},
		Default:  true,
		Priority: 120,
		Source:   "auto",
	}
	res.RunConfigurations = append(res.RunConfigurations, cfg)
	res.RunConfigurations = append(res.RunConfigurations, ideConfigs(in, m)...)
	return res, nil
}

// ideConfigs 转换 PyCharm 的 Django 运行配置（Python.DjangoServer）。
func ideConfigs(in *sdk.AnalyzeParams, m *Model) []sdk.RunConfiguration {
	var out []sdk.RunConfiguration
	for _, c := range in.IdeConfigs {
		if c.Source != "idea" || c.Type != "Python.DjangoServer" {
			continue
		}
		opts := map[string]any{"host": "127.0.0.1", "port": defaultPort, "manage": m.Manage}
		if h := c.Options["host"]; h != "" {
			opts["host"] = h
		}
		if pt, err := strconv.Atoi(c.Options["port"]); err == nil && pt > 0 {
			opts["port"] = pt
		}
		if c.Options["customRunCommand"] != "" || c.Options["additionalOptions"] != "" {
			opts["args"] = strings.TrimSpace(c.Options["additionalOptions"])
		}
		if c.Options["noreload"] == "true" {
			opts["noreload"] = true
		}
		out = append(out, sdk.RunConfiguration{ID: sdk.StableID("idea", c.Type, c.Name), Name: c.Name, Type: "django-server", Options: opts, Env: c.Env, Source: "idea", Priority: 130})
	}
	return out
}

// managePath 返回 manage.py 的绝对路径与所在目录。
func managePath(module sdk.ModuleInfo, opts map[string]any) (string, string, error) {
	manage := sdk.OptString(opts, "manage", "")
	if manage == "" {
		manage = analyzeModule(module.Dir, module.Facet("django")).Manage
	}
	if manage == "" {
		return "", "", os.ErrNotExist
	}
	if !filepath.IsAbs(manage) {
		manage = filepath.Join(module.Dir, manage)
	}
	if !sdk.FileExists(manage) {
		return "", "", os.ErrNotExist
	}
	return manage, filepath.Dir(manage), nil
}

// PrepareRun：python manage.py runserver host:port。
func (p *Plugin) PrepareRun(ctx *sdk.Context, in *sdk.PrepareRunParams) (*sdk.LaunchSpec, error) {
	opts := in.Config.Options
	manage, manageDir, err := managePath(in.Module, opts)
	if err != nil {
		return nil, sdk.UserError("%s", ctx.T("err.noManage"))
	}
	r, err := resolvePython(ctx, in.Module, in.Toolchains, in.Env, in.RunID, true, envFileOpt(opts))
	if err != nil {
		return nil, err
	}
	env := r.Env
	if s := sdk.OptString(opts, "settings", ""); s != "" {
		env["DJANGO_SETTINGS_MODULE"] = s
	}
	for k, v := range optEnv(opts) {
		env[k] = v
	}
	// manage.py 所在目录要在 PYTHONPATH 里（它在子目录时，模块根不够）
	if pp := env["PYTHONPATH"]; !strings.Contains(pp, manageDir) {
		env["PYTHONPATH"] = manageDir + string(os.PathListSeparator) + pp
	}
	host := sdk.OptString(opts, "host", "127.0.0.1")
	port := optInt(opts, "port", defaultPort)
	addr := host + ":" + strconv.Itoa(port)
	if strings.Contains(host, ":") { // IPv6
		addr = "[" + host + "]:" + strconv.Itoa(port)
	}
	args := []string{manage, "runserver", addr}
	if sdk.OptBool(opts, "noreload", false) {
		args = append(args, "--noreload")
	}
	args = append(args, sdk.SplitArgs(sdk.OptString(opts, "args", ""))...)
	return &sdk.LaunchSpec{
		Command:     r.Python,
		Args:        args,
		Cwd:         workingDir(in.Module.Dir, opts, manageDir),
		Env:         env,
		PathPrepend: r.PathPrepend,
		Ports:       []int{port},
		StopSignal:  "SIGINT",
	}, nil
}

var portBusyRe = regexp.MustCompile(`That port is already in use|Address already in use|\[WinError 10048\]`)

// Diagnose：runserver 的「That port is already in use.」不带端口号，用运行配置的端口生成修复。
func (p *Plugin) Diagnose(ctx *sdk.Context, in *sdk.DiagnoseParams) (*sdk.DiagnoseResult, error) {
	if !in.Module.HasFacet("django") {
		return &sdk.DiagnoseResult{}, nil
	}
	return &sdk.DiagnoseResult{Diagnoses: portDiagnoses(ctx, in, defaultPort, portBusyRe)}, nil
}

// migrate 执行 manage.py migrate；console 为空时捕获输出放进错误/消息。
func (p *Plugin) migrate(ctx *sdk.Context, module sdk.ModuleInfo, tcs map[string]sdk.Toolchain, env map[string]string, opts map[string]any, console string) (string, error) {
	manage, manageDir, err := managePath(module, opts)
	if err != nil {
		return "", sdk.UserError("%s", ctx.T("err.noManage"))
	}
	r, err := resolvePython(ctx, module, tcs, env, console, console != "", envFileOpt(opts))
	if err != nil {
		return "", err
	}
	e := sdk.MergeEnv(env, r.Env)
	if s := sdk.OptString(opts, "settings", ""); s != "" {
		e["DJANGO_SETTINGS_MODULE"] = s
	}
	res, err := ctx.Host.Exec(ctx, &sdk.ExecParams{Command: r.Python, Args: []string{manage, "migrate", "--noinput"}, Cwd: manageDir, Env: e, PathPrepend: r.PathPrepend, Console: console, Capture: console == "", Title: "migrate"})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if res.ExitCode != 0 {
		if len(out) > 1500 {
			out = "…" + out[len(out)-1500:]
		}
		return "", sdk.UserError("%s", ctx.T("err.migrate", res.ExitCode, out))
	}
	return out, nil
}

// ApplyFix：执行 migrate / 安装驱动（输出进修复任务的控制台）。
func (p *Plugin) ApplyFix(ctx *sdk.Context, in *sdk.ApplyFixParams) (*sdk.ApplyFixResult, error) {
	switch in.Fix.ID {
	case "migrate":
		var opts map[string]any
		if in.Config != nil {
			opts = in.Config.Options
		}
		if _, err := p.migrate(ctx, in.Module, in.Toolchains, in.Env, opts, in.TaskID); err != nil {
			return nil, err
		}
		return &sdk.ApplyFixResult{Rerun: true, Message: ctx.T("msg.migrated")}, nil
	case "installPackage":
		pkg := in.Fix.Params["package"]
		if pkg == "" {
			return nil, sdk.ErrNotSupported
		}
		if err := installPythonPackages(ctx, in.Module, in.Toolchains, in.Env, in.TaskID, pkg); err != nil {
			return nil, err
		}
		return &sdk.ApplyFixResult{Rerun: true, Message: ctx.T("msg.installed", pkg)}, nil
	}
	return nil, sdk.ErrNotSupported
}

// InvokeAction：执行 migrate（输出进任务控制台）。
func (p *Plugin) InvokeAction(ctx *sdk.Context, in *sdk.InvokeActionParams) (*sdk.InvokeActionResult, error) {
	if in.Action != "migrate" {
		return nil, sdk.ErrNotSupported
	}
	if _, err := p.migrate(ctx, in.Module, in.Toolchains, in.Env, in.Args, in.TaskID); err != nil {
		return nil, err
	}
	return &sdk.InvokeActionResult{Message: ctx.T("msg.migrated")}, nil
}
