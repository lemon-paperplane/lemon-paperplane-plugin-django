# lemon-paperplane-plugin-django

Lemon Paperplane 的 Django 插件（依赖 python 插件）。

- 识别 manage.py（工程根或下两层）与 `DJANGO_SETTINGS_MODULE`，列出 settings 包里的各个设置模块。
- 运行配置 `django-server`：`python manage.py runserver 127.0.0.1:<port>`，可选设置模块、关闭重载、`.env`。
- 识别「development server at http://…」地址；「You have N unapplied migration(s)」给出「执行 migrate」修复（需确认），也可从菜单 / 运行工具栏执行。
- 诊断：端口占用（换端口）、no such table、未安装 Django、MySQL / PostgreSQL 驱动、SECRET_KEY、数据库连不上。

解释器与虚拟环境由 python 插件的 `resolveEnv` 服务提供。构建：`./build.sh`；测试：`go test ./...`。
