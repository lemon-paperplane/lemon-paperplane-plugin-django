// paperplane-plugin-django 是 Django 插件的独立可执行入口。
package main

import (
	sdk "github.com/lemon-paperplane/lemon-paperplane-main/lemon-paperplane-main-plugin-sdk"
	django "github.com/lemon-paperplane/lemon-paperplane-plugin-django/plugin"
)

func main() { sdk.Serve(django.New()) }
