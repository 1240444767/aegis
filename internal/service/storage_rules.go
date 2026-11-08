package service

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// 上传规则：存储中心「上传规则」里配的那几类，在每次写入时真正执行。
//
// 规则按作用范围叠加：config_id / app_id 为空表示对所有存储 / 所有应用生效。
// 每条规则还可以用 uploaderTypes 限定只管哪类上传者（user / admin），
// 免得一条「只许传图片」把云存储的 JSON 写入、发布中心的安装包一起拦掉。
//
// 规则数据（rule_data）的形状：
//
//	upload_limit  {"maxBytes": 10485760}
//	file_type     {"mode": "allow"|"deny", "extensions": [".png"], "mimeTypes": ["image/*"]}
//	path_pattern  {"mode": "allow"|"deny", "patterns": ["avatars/**", "**/*.exe"]}
//	quota         {"maxBytes": 1073741824, "maxFiles": 10000}

const (
	RuleTypeUploadLimit = "upload_limit"
	RuleTypeFileType    = "file_type"
	RuleTypePathPattern = "path_pattern"
	RuleTypeQuota       = "quota"
)

// storageRuleSpec 解码后的规则数据。
type storageRuleSpec struct {
	MaxBytes      int64    `json:"maxBytes"`
	MaxFiles      int64    `json:"maxFiles"`
	Mode          string   `json:"mode"`
	Extensions    []string `json:"extensions"`
	MimeTypes     []string `json:"mimeTypes"`
	Patterns      []string `json:"patterns"`
	UploaderTypes []string `json:"uploaderTypes"`
}

// NormalizeStorageRule 校验并规范化规则数据，返回写库用的形态。
func NormalizeStorageRule(ruleType string, data map[string]any) (map[string]any, error) {
	var spec storageRuleSpec
	if err := decodeStorageConfigData(data, &spec); err != nil {
		return nil, apperrors.New(42277, http.StatusUnprocessableEntity, "规则数据格式不正确")
	}
	invalid := func(message string) error {
		return apperrors.New(42277, http.StatusUnprocessableEntity, message)
	}
	out := map[string]any{}
	types := normalizeLowerList(spec.UploaderTypes)
	for _, t := range types {
		if t != "user" && t != "admin" {
			return nil, invalid("上传者类型只能是 user 或 admin")
		}
	}
	if len(types) > 0 {
		out["uploaderTypes"] = types
	}
	switch ruleType {
	case RuleTypeUploadLimit:
		if spec.MaxBytes <= 0 {
			return nil, invalid("单文件上限需大于 0")
		}
		out["maxBytes"] = spec.MaxBytes
	case RuleTypeFileType:
		mode := normalizeRuleMode(spec.Mode)
		if mode == "" {
			return nil, invalid("类型规则的模式只能是 allow（只允许）或 deny（禁止）")
		}
		extensions := make([]string, 0, len(spec.Extensions))
		for _, ext := range normalizeLowerList(spec.Extensions) {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extensions = append(extensions, ext)
		}
		mimes := normalizeLowerList(spec.MimeTypes)
		for _, m := range mimes {
			if !strings.Contains(m, "/") {
				return nil, invalid("MIME 类型应形如 image/png 或 image/*：" + m)
			}
		}
		if len(extensions) == 0 && len(mimes) == 0 {
			return nil, invalid("至少填写一个扩展名或 MIME 类型")
		}
		out["mode"], out["extensions"], out["mimeTypes"] = mode, extensions, mimes
	case RuleTypePathPattern:
		mode := normalizeRuleMode(spec.Mode)
		if mode == "" {
			return nil, invalid("路径规则的模式只能是 allow（只允许）或 deny（禁止）")
		}
		patterns := make([]string, 0, len(spec.Patterns))
		for _, p := range spec.Patterns {
			p = strings.Trim(strings.TrimSpace(p), "/")
			if p == "" {
				continue
			}
			if _, err := globToRegexp(p); err != nil {
				return nil, invalid("路径模式无效：" + p)
			}
			patterns = append(patterns, p)
		}
		if len(patterns) == 0 {
			return nil, invalid("至少填写一个路径模式")
		}
		out["mode"], out["patterns"] = mode, patterns
	case RuleTypeQuota:
		if spec.MaxBytes <= 0 && spec.MaxFiles <= 0 {
			return nil, invalid("配额至少设置总大小或文件数之一")
		}
		if spec.MaxBytes < 0 || spec.MaxFiles < 0 {
			return nil, invalid("配额不能为负数")
		}
		out["maxBytes"], out["maxFiles"] = spec.MaxBytes, spec.MaxFiles
	default:
		return nil, invalid("不支持的规则类型")
	}
	return out, nil
}

// enforceUploadRules 写入前执行规则。input 的类型、对象键已经定型（魔数识别、拼好 root_path）。
func (s *StorageService) enforceUploadRules(ctx context.Context, appID int64, cfg *storagedomain.Config, input storagedomain.UploadInput) error {
	rules, err := s.pg.GetActiveUploadRules(ctx, cfg.ID, &appID)
	if err != nil {
		return err
	}
	uploader := input.UploaderType
	if uploader == "" {
		uploader = "user"
	}
	for _, rule := range rules {
		var spec storageRuleSpec
		if err := decodeStorageConfigData(rule.RuleData, &spec); err != nil {
			continue
		}
		if types := normalizeLowerList(spec.UploaderTypes); len(types) > 0 && !containsString(types, uploader) {
			continue
		}
		switch rule.RuleType {
		case RuleTypeUploadLimit:
			if spec.MaxBytes > 0 && input.ContentLength > spec.MaxBytes {
				return apperrors.New(41300, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("文件超过上传上限 %s", formatStorageBytes(spec.MaxBytes)))
			}
		case RuleTypeFileType:
			matched := matchFileType(spec, input.FileName, input.ContentType)
			mode := normalizeRuleMode(spec.Mode)
			if (mode == "allow" && !matched) || (mode == "deny" && matched) {
				return apperrors.New(41500, http.StatusUnsupportedMediaType, "不允许上传这种类型的文件")
			}
		case RuleTypePathPattern:
			matched := false
			for _, pattern := range spec.Patterns {
				if re, err := globToRegexp(strings.Trim(pattern, "/")); err == nil && re.MatchString(input.ObjectKey) {
					matched = true
					break
				}
			}
			mode := normalizeRuleMode(spec.Mode)
			if (mode == "allow" && !matched) || (mode == "deny" && matched) {
				return apperrors.New(42278, http.StatusUnprocessableEntity, "不允许写入这个路径")
			}
		case RuleTypeQuota:
			scopeApp := appID
			if rule.AppID != nil {
				scopeApp = *rule.AppID
			}
			bytes, files, err := s.pg.StorageObjectTotals(ctx, scopeApp, rule.ConfigID)
			if err != nil {
				return err
			}
			if spec.MaxBytes > 0 && bytes+input.ContentLength > spec.MaxBytes {
				return apperrors.New(41301, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("存储配额不足：已用 %s，上限 %s", formatStorageBytes(bytes), formatStorageBytes(spec.MaxBytes)))
			}
			if spec.MaxFiles > 0 && files+1 > spec.MaxFiles {
				return apperrors.New(41301, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("文件数已达上限 %d", spec.MaxFiles))
			}
		}
	}
	return nil
}

// matchFileType 扩展名或 MIME 任一命中即算命中。MIME 支持 image/* 这样的大类通配。
func matchFileType(spec storageRuleSpec, fileName, contentType string) bool {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(fileName)))
	for _, candidate := range spec.Extensions {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if !strings.HasPrefix(candidate, ".") {
			candidate = "." + candidate
		}
		if ext != "" && ext == candidate {
			return true
		}
	}
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
		mediaType = parsed
	}
	for _, pattern := range spec.MimeTypes {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if strings.HasSuffix(pattern, "/*") {
			if strings.HasPrefix(mediaType, strings.TrimSuffix(pattern, "*")) {
				return true
			}
		} else if pattern == mediaType {
			return true
		}
	}
	return false
}

// globToRegexp `*` 不跨目录，`**` 跨任意层目录，`?` 一个字符。
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func normalizeRuleMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "allow":
		return "allow"
	case "deny":
		return "deny"
	}
	return ""
}

func normalizeLowerList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
