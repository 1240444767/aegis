package httptransport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 原生 SDK 换会话的请求体照 Kotlin SDK（AegisAuthApi.oauthExchange）实际发出的形状写：
// 网关的应用在路径上，请求体里没有 appid。此前复用旧命名空间的 DTO，appid 必填，
// 这样的请求一律 400「缺少必要的请求参数」—— QQ 登录在 App 上从来没成功过。
func TestAppOAuthExchangeRequestBindsSDKBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo/auth/oauth/exchange", strings.NewReader(
		`{"provider":"qq","accessToken":"AT","openId":"OPENID","deferRegistration":true,"deviceId":"dev-1","device":"Pixel 9"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req AppOAuthExchangeRequest
	if err := bind(ctx, &req); err != nil {
		t.Fatalf("SDK 的请求体应当能绑定：%v", err)
	}
	if req.Provider != "qq" || req.AccessToken != "AT" || req.claimedOpenID() != "OPENID" || !req.DeferRegistration ||
		req.DeviceID != "dev-1" || req.Device != "Pixel 9" {
		t.Fatalf("绑定结果不对：%+v", req)
	}

	// 老客户端只带 providerUserId 也照样核对
	legacy := AppOAuthExchangeRequest{ProviderUserID: " OLD "}
	if legacy.claimedOpenID() != "OLD" {
		t.Fatalf("应兼容 providerUserId，得到 %q", legacy.claimedOpenID())
	}

	// 缺令牌仍然要拒
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo/auth/oauth/exchange", strings.NewReader(`{"provider":"qq"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	var missing AppOAuthExchangeRequest
	if err := bind(ctx, &missing); err == nil {
		t.Fatal("缺 accessToken 应当绑定失败")
	}
}

// 网关接口的应用一律来自路径上的 appKey。任何一个网关处理器绑定了「appid 必填」的请求体，
// 客户端照协议发请求就会 400「缺少必要的请求参数」，而协议文档、SDK 都看不出问题。
//
// 这里直接读本包源码：从 router_gateway.go 找出全部网关处理器（含它们转调的 h.X(c)），
// 再看它们用到的结构体里有没有 appid 必填的。
func TestGatewayHandlersDoNotRequireBodyAppID(t *testing.T) {
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", path, err)
		}
		files[path] = file
	}

	// 1. appid 必填的结构体
	requiresAppID := map[string]bool{}
	methods := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			switch typed := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range typed.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						if field.Tag == nil {
							continue
						}
						tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
						name := strings.Split(tag.Get("json"), ",")[0]
						if strings.EqualFold(name, "appid") && strings.Contains(tag.Get("binding"), "required") {
							requiresAppID[ts.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if typed.Recv != nil && len(typed.Recv.List) == 1 {
					if star, ok := typed.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := star.X.(*ast.Ident); ok && ident.Name == "Handler" {
							methods[typed.Name.Name] = typed
						}
					}
				}
			}
		}
	}
	if len(requiresAppID) == 0 {
		t.Fatal("没找到任何 appid 必填的结构体，扫描规则可能已经失效")
	}

	// 2. 网关处理器
	routerSource, err := os.ReadFile("router_gateway.go")
	if err != nil {
		t.Fatal(err)
	}
	router := files["router_gateway.go"]
	handlers := map[string]bool{}
	ast.Inspect(router, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		group, ok := sel.X.(*ast.Ident)
		if !ok || !strings.HasPrefix(group.Name, "appGateway") {
			return true
		}
		for _, arg := range call.Args {
			if handler, ok := arg.(*ast.SelectorExpr); ok {
				if recv, ok := handler.X.(*ast.Ident); ok && recv.Name == "h" {
					handlers[handler.Sel.Name] = true
				}
			}
		}
		return true
	})
	if len(handlers) < 20 {
		t.Fatalf("只从 router_gateway.go 找到 %d 个网关处理器，扫描规则可能已经失效（源码 %d 字节）", len(handlers), len(routerSource))
	}

	// 3. 处理器（及其转调的 h.X(c)）里用到的类型
	var offenders []string
	for name := range handlers {
		visited := map[string]bool{}
		var walk func(method string, depth int)
		walk = func(method string, depth int) {
			if visited[method] || depth > 2 {
				return
			}
			visited[method] = true
			decl := methods[method]
			if decl == nil || decl.Body == nil {
				return
			}
			ast.Inspect(decl.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.Ident:
					if requiresAppID[typed.Name] {
						offenders = append(offenders, name+" → "+typed.Name)
					}
				case *ast.CallExpr:
					if sel, ok := typed.Fun.(*ast.SelectorExpr); ok {
						if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == "h" {
							walk(sel.Sel.Name, depth+1)
						}
					}
				}
				return true
			})
		}
		walk(name, 0)
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("网关处理器用到了 appid 必填的请求体，客户端照协议调用会 400：\n%s", strings.Join(offenders, "\n"))
	}
}
