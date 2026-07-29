// Copyright 2026 Alibaba Group
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/errors"
	"github.com/DingTalk-Real-AI/dingtalk-workspace-cli/internal/shortcut"
)

const resourceDownloadTimeout = 10 * time.Minute

var (
	resourceGetwd        = os.Getwd
	resourceAbs          = filepath.Abs
	resourceEvalSymlinks = filepath.EvalSymlinks
	resourceStat         = os.Stat
	resourceLstat        = os.Lstat
	resourceRel          = filepath.Rel
	resourceMkdir        = os.Mkdir
	resourceCreateTemp   = os.CreateTemp
	resourceCopy         = io.Copy
	resourceTempSync     = (*os.File).Sync
	resourceTempClose    = (*os.File).Close
	resourceRename       = os.Rename
	resourceLink         = os.Link
	resourceDownload     = downloadResourceAtomically
)

// MessagesResourceDownload resolves a temporary IM resource URL and saves the
// bytes through a safe, atomic, no-clobber local-file workflow.
var MessagesResourceDownload = shortcut.Shortcut{
	Service:     "chat",
	Command:     "+messages-resource-download",
	Product:     "im",
	Description: "安全下载消息资源（图片/视频/语音）到本地",
	Intent: "当你需要拿到消息里的实际图片、视频或语音文件，而不只是临时 URL 时使用；" +
		"先用消息和会话身份换取下载地址，再安全写入工作目录内的相对路径。" +
		"默认不覆盖已有文件，只有显式传 --overwrite 才覆盖。",
	Risk: shortcut.RiskRead,
	Flags: []shortcut.Flag{
		{Name: "type", Type: shortcut.FlagString, Default: "mediaId", Desc: "资源类型", Enum: []string{"mediaId"}},
		{Name: "resource-id", Type: shortcut.FlagString, Desc: "资源 ID（消息中的 mediaId）", Required: true},
		{Name: "message-id", Type: shortcut.FlagString, Desc: "消息 openMessageId", Required: true},
		{Name: "open-conversation-id", Type: shortcut.FlagString, Desc: "会话 openConversationId", Required: true},
		{Name: "output", Type: shortcut.FlagString, Default: ".", Desc: "工作目录内的相对文件或目录路径"},
		{Name: "overwrite", Type: shortcut.FlagBool, Desc: "允许覆盖已存在的目标文件（默认拒绝）"},
	},
	Constraints: []shortcut.Constraint{
		{
			Kind:        shortcut.ConstraintCustom,
			Flags:       []string{"output"},
			Description: "--output 必须是工作目录内的相对路径，不允许绝对路径或 .. 逃逸",
		},
	},
	Tips: []string{
		`dws chat +messages-resource-download --resource-id <mediaId> --message-id <openMessageId> --open-conversation-id <openConversationId>`,
		`dws chat +messages-resource-download --resource-id <mediaId> --message-id <openMessageId> --open-conversation-id <openConversationId> --output ./downloads/`,
	},
	Validate: func(rt *shortcut.RuntimeContext) error {
		return validateResourceDownloadOutput(rt.Str("output"))
	},
	Execute: func(rt *shortcut.RuntimeContext) error {
		plan := map[string]any{
			"resourceType":       rt.Str("type"),
			"resourceId":         rt.Str("resource-id"),
			"messageId":          rt.Str("message-id"),
			"openConversationId": rt.Str("open-conversation-id"),
			"output":             rt.Str("output"),
			"overwrite":          rt.Bool("overwrite"),
		}
		if rt.DryRun() {
			plan["dryRun"] = true
			plan["steps"] = []string{
				"resolve temporary download URL",
				"validate output path and no-clobber policy",
				"download to a temporary file",
				"atomically publish the completed file",
			}
			return rt.Output(plan)
		}

		data, err := rt.CallMCPData("im", "get_resource_download_url", map[string]any{
			"resourceType":       rt.Str("type"),
			"resourceId":         rt.Str("resource-id"),
			"openMessageId":      rt.Str("message-id"),
			"openConversationId": rt.Str("open-conversation-id"),
		})
		if err != nil {
			return err
		}
		resourceURL, headers, err := resourceDownloadInfo(data)
		if err != nil {
			return err
		}
		cwd, err := resourceGetwd()
		if err != nil {
			return apperrors.NewInternal(fmt.Sprintf("读取工作目录失败: %v", err))
		}
		destPath, relativePath, err := resolveResourceDownloadPath(
			cwd, rt.Str("output"), resourceURL, rt.Bool("overwrite"))
		if err != nil {
			return err
		}
		size, err := resourceDownload(
			rt.Command().Context(), nil, resourceURL, headers, destPath, rt.Bool("overwrite"))
		if err != nil {
			return err
		}
		return rt.Output(map[string]any{
			"messageId":    rt.Str("message-id"),
			"resourceId":   rt.Str("resource-id"),
			"resourceType": rt.Str("type"),
			"localPath":    filepath.ToSlash(relativePath),
			"sizeBytes":    size,
		})
	},
}

func validateResourceDownloadOutput(output string) error {
	return validateResourceDownloadOutputFlag(output, "--output")
}

func validateResourceDownloadOutputFlag(output, flagName string) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return apperrors.NewValidation(flagName + " 不能为空")
	}
	if filepath.IsAbs(output) {
		return apperrors.NewValidation(flagName + " 只接受工作目录内的相对路径")
	}
	clean := filepath.Clean(output)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return apperrors.NewValidation(flagName + " 不允许使用 .. 逃逸工作目录")
	}
	return nil
}

func resourceDownloadInfo(data map[string]any) (string, map[string]string, error) {
	for {
		result, ok := data["result"].(map[string]any)
		if !ok {
			break
		}
		data = result
	}

	resourceURL := ""
	switch value := data["resourceUrl"].(type) {
	case string:
		resourceURL = strings.TrimSpace(value)
	case []any:
		for _, item := range value {
			if candidate, ok := item.(string); ok && strings.TrimSpace(candidate) != "" {
				resourceURL = strings.TrimSpace(candidate)
				break
			}
		}
	}
	if resourceURL == "" {
		if value, ok := data["downloadUrl"].(string); ok {
			resourceURL = strings.TrimSpace(value)
		}
	}
	parsed, err := url.Parse(resourceURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return "", nil, apperrors.NewAPI(
			"资源下载接口未返回合法的 HTTPS 下载地址")
	}
	if parsed.Scheme == "http" && isAliyunOSSHost(parsed.Hostname()) {
		// The IM backend currently returns a signed plaintext OSS URL even
		// though the same object endpoint supports TLS. Upgrade only the
		// official OSS hostname; never rewrite arbitrary HTTP origins.
		parsed.Scheme = "https"
		resourceURL = parsed.String()
	}
	if parsed.Scheme != "https" {
		return "", nil, apperrors.NewAPI(
			"资源下载接口未返回合法的 HTTPS 下载地址")
	}

	headers := map[string]string{}
	if values, ok := data["headers"].(map[string]any); ok {
		for key, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(key) != "" {
				headers[key] = text
			}
		}
	}
	return resourceURL, headers, nil
}

func isAliyunOSSHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "aliyuncs.com" || strings.HasSuffix(host, ".aliyuncs.com")
}

func resolveResourceDownloadPath(
	baseDir, output, resourceURL string,
	overwrite bool,
) (absolutePath, relativePath string, err error) {
	if err := validateResourceDownloadOutput(output); err != nil {
		return "", "", err
	}
	baseDir, err = resourceAbs(baseDir)
	if err != nil {
		return "", "", apperrors.NewInternal(fmt.Sprintf("解析工作目录失败: %v", err))
	}
	realBase, err := resourceEvalSymlinks(baseDir)
	if err != nil {
		return "", "", apperrors.NewInternal(fmt.Sprintf("解析工作目录失败: %v", err))
	}

	rawOutput := strings.TrimSpace(output)
	directoryIntent := strings.HasSuffix(rawOutput, string(os.PathSeparator)) ||
		strings.HasSuffix(rawOutput, "/")
	output = filepath.Clean(rawOutput)
	candidate := filepath.Join(realBase, output)
	info, statErr := resourceStat(candidate)
	isDirectory := (statErr == nil && info.IsDir()) ||
		directoryIntent ||
		output == "."
	if isDirectory {
		candidate = filepath.Join(candidate, resourceDownloadFilename(resourceURL))
	}

	parent := filepath.Dir(candidate)
	if err := ensureResourceDownloadParent(realBase, parent); err != nil {
		return "", "", err
	}
	realParent, err := resourceEvalSymlinks(parent)
	if err != nil {
		return "", "", apperrors.NewInternal(fmt.Sprintf("解析输出目录失败: %v", err))
	}
	parentRel, err := resourceRel(realBase, realParent)
	if err != nil || parentRel == ".." ||
		strings.HasPrefix(parentRel, ".."+string(os.PathSeparator)) {
		return "", "", apperrors.NewValidation("--output 解析后逃逸工作目录")
	}

	absolutePath = filepath.Join(realParent, filepath.Base(candidate))
	if info, statErr := resourceLstat(absolutePath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "", apperrors.NewValidation("--output 目标不能是符号链接")
		}
		if info.IsDir() {
			return "", "", apperrors.NewValidation("--output 目标是目录，无法写入文件")
		}
		if !overwrite {
			return "", "", apperrors.NewValidation(
				"目标文件已存在；如确认覆盖请显式传 --overwrite")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", apperrors.NewInternal(fmt.Sprintf("检查输出文件失败: %v", statErr))
	}
	relativePath, err = resourceRel(realBase, absolutePath)
	if err != nil {
		return "", "", apperrors.NewInternal(fmt.Sprintf("解析输出相对路径失败: %v", err))
	}
	return absolutePath, relativePath, nil
}

func ensureResourceDownloadParent(baseDir, parent string) error {
	relative, err := resourceRel(baseDir, parent)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return apperrors.NewValidation("--output 解析后逃逸工作目录")
	}
	if relative == "." {
		return nil
	}

	current := baseDir
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, statErr := resourceLstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := resourceMkdir(current, 0o755); mkdirErr != nil {
				// A concurrent creator may have won the race. Re-check the
				// resulting entry instead of following it implicitly.
				info, statErr = resourceLstat(current)
				if statErr != nil {
					return apperrors.NewInternal(fmt.Sprintf(
						"创建输出目录失败: %v", mkdirErr))
				}
			} else {
				continue
			}
		} else if statErr != nil {
			return apperrors.NewInternal(fmt.Sprintf(
				"检查输出目录失败: %v", statErr))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return apperrors.NewValidation("--output 父目录不能是符号链接")
		}
		if !info.IsDir() {
			return apperrors.NewValidation("--output 父路径不是目录")
		}
	}
	return nil
}

func resourceDownloadFilename(resourceURL string) string {
	parsed, err := url.Parse(resourceURL)
	if err == nil {
		name, unescapeErr := url.PathUnescape(filepath.Base(parsed.Path))
		if unescapeErr == nil {
			name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
			if name != "" && name != "." && name != string(os.PathSeparator) {
				return name
			}
		}
	}
	return "download"
}

func downloadResourceAtomically(
	ctx context.Context,
	client *http.Client,
	resourceURL string,
	headers map[string]string,
	destPath string,
	overwrite bool,
) (size int64, err error) {
	if client == nil {
		client = &http.Client{Timeout: resourceDownloadTimeout}
	}
	clientCopy := *client
	client = &clientCopy
	originalRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || strings.TrimSpace(req.URL.Host) == "" {
			return apperrors.NewValidation("资源下载重定向必须使用 HTTPS")
		}
		if len(via) >= 10 {
			return apperrors.NewAPI("资源下载重定向次数过多")
		}
		if originalRedirect != nil {
			return originalRedirect(req, via)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return 0, apperrors.NewValidation(fmt.Sprintf("创建资源下载请求失败: %v", err))
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, apperrors.NewAPI(fmt.Sprintf("下载消息资源失败: %v", err))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, apperrors.NewAPI(fmt.Sprintf(
			"下载消息资源失败: HTTP %d", response.StatusCode))
	}

	parent := filepath.Dir(destPath)
	temp, err := resourceCreateTemp(parent, "."+filepath.Base(destPath)+".part-*")
	if err != nil {
		return 0, apperrors.NewInternal(fmt.Sprintf("创建下载临时文件失败: %v", err))
	}
	tempPath := temp.Name()
	defer func() {
		_ = resourceTempClose(temp)
		_ = os.Remove(tempPath)
	}()

	size, err = resourceCopy(temp, response.Body)
	if err != nil {
		return 0, apperrors.NewAPI(fmt.Sprintf("写入消息资源失败: %v", err))
	}
	if response.ContentLength >= 0 && size != response.ContentLength {
		return 0, apperrors.NewAPI(fmt.Sprintf(
			"消息资源大小校验失败: 下载 %d 字节，期望 %d 字节",
			size, response.ContentLength))
	}
	if err := resourceTempSync(temp); err != nil {
		return 0, apperrors.NewInternal(fmt.Sprintf("同步消息资源失败: %v", err))
	}
	if err := resourceTempClose(temp); err != nil {
		return 0, apperrors.NewInternal(fmt.Sprintf("关闭消息资源失败: %v", err))
	}
	if overwrite {
		if err := resourceRename(tempPath, destPath); err != nil {
			return 0, apperrors.NewInternal(fmt.Sprintf("发布消息资源失败: %v", err))
		}
		return size, nil
	}
	if err := resourceLink(tempPath, destPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, apperrors.NewValidation(
				"目标文件已存在；如确认覆盖请显式传 --overwrite")
		}
		return 0, apperrors.NewInternal(fmt.Sprintf("发布消息资源失败: %v", err))
	}
	return size, nil
}

func init() {
	shortcut.Register(MessagesResourceDownload)
}
