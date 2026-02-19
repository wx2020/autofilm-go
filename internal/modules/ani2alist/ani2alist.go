package ani2alist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// ANI季度月份
var aniSeasonMonths = []int{1, 4, 7, 10}

// Config Ani2Alist配置
type Config struct {
	ID        string
	URL       string
	Username  string
	Password  string
	Token     string
	TargetDir string
	RSSUpdate bool
	Year      *int
	Month     *int
	SrcDomain string
	RSSDomain string
	KeyWord   string
	Cron      string
}

// Ani2Alist ANI转Alist处理器
type Ani2Alist struct {
	config     *Config
	client     *alist.AlistClient
	logger     *logrus.Logger
	year       int
	month      int
	keyWord    string
	rssUpdate  bool
	srcDomain  string
	rssDomain  string
}

// New 创建新的Ani2Alist实例
func New(cfg *Config) (*Ani2Alist, error) {
	client, err := alist.GetClient(cfg.URL, cfg.Username, cfg.Password, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("创建Alist客户端失败: %w", err)
	}

	a2a := &Ani2Alist{
		config:    cfg,
		client:    client,
		logger:    core.GetLogger(),
		rssUpdate: cfg.RSSUpdate,
		keyWord:   cfg.KeyWord,
		srcDomain: strings.TrimSpace(cfg.SrcDomain),
		rssDomain: strings.TrimSpace(cfg.RSSDomain),
	}

	// 设置默认值
	if a2a.srcDomain == "" {
		a2a.srcDomain = "aniopen.an-i.workers.dev"
	}
	if a2a.rssDomain == "" {
		a2a.rssDomain = "api.ani.rip"
	}

	// 解析时间参数
	if cfg.RSSUpdate {
		a2a.logger.Debug("使用RSS追更最新番剧")
	} else if cfg.KeyWord != "" {
		a2a.logger.Debugf("使用自定义关键字: %s", cfg.KeyWord)
		a2a.keyWord = cfg.KeyWord
	} else if cfg.Year != nil && cfg.Month != nil {
		a2a.year = *cfg.Year
		a2a.month = *cfg.Month
	} else {
		// 使用当前季度
		now := time.Now()
		a2a.year = now.Year()
		a2a.month = int(now.Month())
		a2a.logger.Info("未传入时间参数，默认使用当前季度")
	}

	return a2a, nil
}

// Run 运行Ani2Alist处理
func (a2a *Ani2Alist) Run(ctx context.Context) error {
	// 验证参数
	if valid, errMsg := a2a.isValid(); !valid {
		a2a.logger.Error(errMsg)
		return fmt.Errorf(errMsg)
	}

	// 获取或创建存储器
	targetDir := "/" + strings.Trim(a2a.config.TargetDir, "/")
	storage, err := a2a.client.GetStorageByMountPath(ctx, targetDir, true, "UrlTree")
	if err != nil {
		return fmt.Errorf("获取存储器失败: %w", err)
	}

	// 获取当前URL结构
	additionDict, err := storage.Addition2dict()
	if err != nil {
		return fmt.Errorf("解析存储器配置失败: %w", err)
	}

	urlDict := a2a.structure2Dict(additionDict["url_structure"].(string))

	// 更新URL字典
	if err := a2a.updateURLDicts(ctx, urlDict); err != nil {
		return err
	}

	// 保存更新后的配置
	additionDict["url_structure"] = a2a.dict2Structure(urlDict)
	if err := storage.SetAdditionByDict(additionDict); err != nil {
		return fmt.Errorf("设置存储器配置失败: %w", err)
	}

	// 更新存储器
	if err := a2a.client.AdminStorageUpdate(ctx, storage); err != nil {
		return fmt.Errorf("更新存储器失败: %w", err)
	}

	return nil
}

// isValid 验证参数
func (a2a *Ani2Alist) isValid() (bool, string) {
	if a2a.rssUpdate {
		return true, ""
	}

	if a2a.keyWord != "" {
		return true, ""
	}

	if a2a.year == 0 && a2a.month == 0 {
		return true, ""
	}

	// 检查时间范围
	if a2a.year == 2019 && a2a.month == 4 {
		return false, "2019-4季度暂无数据"
	}

	if a2a.year < 2019 || (a2a.year == 2019 && a2a.month < 1) {
		return false, "ANI Open项目仅支持2019年1月及其之后的数据"
	}

	now := time.Now()
	if a2a.year > now.Year() || (a2a.year == now.Year() && a2a.month > int(now.Month())) {
		return false, "传入的年月晚于当前时间"
	}

	return true, ""
}

// updateURLDicts 更新URL字典
func (a2a *Ani2Alist) updateURLDicts(ctx context.Context, urlDict map[string]interface{}) error {
	if a2a.rssUpdate {
		return a2a.updateRSSAnimeDict(ctx, urlDict)
	}
	return a2a.updateSeasonAnimeDict(ctx, urlDict)
}

// getKey 获取季度关键字
func (a2a *Ani2Alist) getKey() string {
	if a2a.keyWord != "" {
		return a2a.keyWord
	}

	year := a2a.year
	month := a2a.month

	// 找到当前季度的起始月份
	for i := month; i > 0; i-- {
		for _, seasonMonth := range aniSeasonMonths {
			if i == seasonMonth {
				return fmt.Sprintf("%d-%d", year, i)
			}
		}
	}

	// 如果没找到，使用当前月份
	return fmt.Sprintf("%d-%d", year, month)
}

// updateSeasonAnimeDict 更新指定季度/关键字的动画列表
func (a2a *Ani2Alist) updateSeasonAnimeDict(ctx context.Context, urlDict map[string]interface{}) error {
	key := a2a.getKey()

	if _, exists := urlDict[key]; !exists {
		urlDict[key] = make(map[string]interface{})
	}

	baseURL := fmt.Sprintf("https://%s/%s/", a2a.srcDomain, key)
	return a2a.updateDataRecursive(ctx, baseURL, urlDict[key].(map[string]interface{}))
}

// updateDataRecursive 递归更新数据
func (a2a *Ani2Alist) updateDataRecursive(ctx context.Context, url string, urlDict map[string]interface{}) error {
	a2a.logger.Debugf("请求地址: %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求发送失败，状态码: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Files []struct {
			Name     string `json:"name"`
			MimeType string `json:"mimeType"`
			Size     string `json:"size"`
			CreatedTime string `json:"createdTime"`
		} `json:"files"`
	}

	if err := json.Unmarshal(body, &result.Files); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	for _, file := range result.Files {
		mimeType := file.MimeType
		name := file.Name

		quotedName := neturl.QueryEscape(name)

		if a2a.isFileMimeType(mimeType) {
			// 文件
			fileURL := url + quotedName + "?d=true"
			size, _ := strconv.ParseInt(file.Size, 10, 64)
			createdTime := a2a.parseTimestamp(file.CreatedTime)

			a2a.logger.Debugf("获取文件: %s，大小: %.2f MB，播放地址: %s",
				name, float64(size)/1024/1024, fileURL)

			urlDict[name] = []interface{}{
				file.Size,
				strconv.FormatInt(createdTime, 10),
				fileURL,
			}

		} else if mimeType == "application/vnd.google-apps.folder" {
			// 目录
			a2a.logger.Debugf("获取目录: %s", name)
			if _, exists := urlDict[name]; !exists {
				urlDict[name] = make(map[string]interface{})
			}
			subDir := urlDict[name].(map[string]interface{})
			if err := a2a.updateDataRecursive(ctx, url+quotedName+"/", subDir); err != nil {
				return err
			}
		} else {
			a2a.logger.Warnf("无法识别类型: %s，文件详情: %+v", mimeType, file)
		}
	}

	return nil
}

// updateRSSAnimeDict 更新RSS动画列表
func (a2a *Ani2Alist) updateRSSAnimeDict(ctx context.Context, urlDict map[string]interface{}) error {
	rssURL := fmt.Sprintf("https://%s/ani-download.xml", a2a.rssDomain)

	req, err := http.NewRequestWithContext(ctx, "GET", rssURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求RSS失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求RSS失败，状态码: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取RSS响应失败: %w", err)
	}

	// 解析RSS
	entries, err := a2a.parseRSS(string(body))
	if err != nil {
		return fmt.Errorf("解析RSS失败: %w", err)
	}

	// 处理RSS条目
	for _, entry := range entries {
		a2a.handleRSSRecursive(urlDict, entry)
	}

	return nil
}

// RSSEntry RSS条目
type RSSEntry struct {
	Title     string
	Link      string
	Published string
	Size      string
}

// parseRSS 解析RSS
func (a2a *Ani2Alist) parseRSS(rssContent string) ([]RSSEntry, error) {
	// 简化的RSS解析
	var entries []RSSEntry

	// 使用正则表达式解析RSS项
	itemRegex := regexp.MustCompile(`<item>(.*?)</item>`)
	items := itemRegex.FindAllStringSubmatch(rssContent, -1)

	for _, item := range items {
		if len(item) < 2 {
			continue
		}

		content := item[1]

		titleRegex := regexp.MustCompile(`<title>(.*?)</title>`)
		titleMatch := titleRegex.FindStringSubmatch(content)
		title := ""
		if len(titleMatch) > 1 {
			title = titleMatch[1]
		}

		linkRegex := regexp.MustCompile(`<link>(.*?)</link>`)
		linkMatch := linkRegex.FindStringSubmatch(content)
		link := ""
		if len(linkMatch) > 1 {
			link = linkMatch[1]
		}

		// 解析anime_size
		sizeRegex := regexp.MustCompile(`<anime_size>(.*?)</anime_size>`)
		sizeMatch := sizeRegex.FindStringSubmatch(content)
		size := ""
		if len(sizeMatch) > 1 {
			size = sizeMatch[1]
		}

		pubDateRegex := regexp.MustCompile(`<pubDate>(.*?)</pubDate>`)
		pubDateMatch := pubDateRegex.FindStringSubmatch(content)
		pubDate := ""
		if len(pubDateMatch) > 1 {
			pubDate = pubDateMatch[1]
		}

		entries = append(entries, RSSEntry{
			Title:     title,
			Link:      link,
			Published: pubDate,
			Size:      size,
		})
	}

	return entries, nil
}

// handleRSSRecursive 递归处理RSS数据
func (a2a *Ani2Alist) handleRSSRecursive(urlDict map[string]interface{}, entry RSSEntry) {
	// 解析URL获取多级目录
	decodedLink, _ := neturl.QueryUnescape(entry.Link)
	parts := strings.Split(decodedLink, "/")
	if len(parts) < 4 {
		return
	}

	parents := parts[3:] // 跳过协议和域名
	currentDict := urlDict

	for i, parent := range parents {
		if i == len(parents)-1 {
			// 最后一个是文件
			timestamp := a2a.parseRSSTimestamp(entry.Published)
			sizeBytes := a2a.convertSizeToBytes(entry.Size)

			urlDict[entry.Title] = []interface{}{
				strconv.FormatInt(sizeBytes, 10),
				strconv.FormatInt(timestamp, 10),
				entry.Link,
			}
		} else {
			// 目录
			if _, exists := currentDict[parent]; !exists {
				currentDict[parent] = make(map[string]interface{})
			}
			var ok bool
			currentDict, ok = currentDict[parent].(map[string]interface{})
			if !ok {
				return
			}
		}
	}
}

// parseTimestamp 解析时间戳
func (a2a *Ani2Alist) parseTimestamp(timeStr string) int64 {
	layout := "2006-01-02T15:04:05.000000Z"
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// parseRSSTimestamp 解析RSS时间戳
func (a2a *Ani2Alist) parseRSSTimestamp(timeStr string) int64 {
	// RSS时间格式: Mon, 02 Jan 2006 15:04:05 MST
	layout := "Mon, 02 Jan 2006 15:04:05 MST"
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// convertSizeToBytes 转换大小为字节
func (a2a *Ani2Alist) convertSizeToBytes(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)
	parts := strings.Split(sizeStr, " ")
	if len(parts) != 2 {
		return 0
	}

	number, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToUpper(parts[1])
	multipliers := map[string]float64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	multiplier, exists := multipliers[unit]
	if !exists {
		return 0
	}

	return int64(number * multiplier)
}

// isFileMimeType 检查是否为文件MIME类型
func (a2a *Ani2Alist) isFileMimeType(mimeType string) bool {
	fileMimeTypes := []string{
		"video/mp4",
		"video/x-matroska",
		"application/octet-stream",
		"application/zip",
	}

	for _, mt := range fileMimeTypes {
		if mimeType == mt {
			return true
		}
	}
	return false
}

// structure2Dict 将Alist地址树结构转换为字典
func (a2a *Ani2Alist) structure2Dict(text string) map[string]interface{} {
	if text == "" {
		return make(map[string]interface{})
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	return a2a.parseLines(lines, 0, 0).result
}

type parseResult struct {
	result map[string]interface{}
	nextIndex int
}

func (a2a *Ani2Alist) parseLines(lines []string, startIndex int, indentLevel int) parseResult {
	result := make(map[string]interface{})
	i := startIndex
	currentFolder := ""

	for i < len(lines) {
		line := lines[i]
		currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))

		if currentIndent > indentLevel {
			// 子项
			subResult := a2a.parseLines(lines, i, currentIndent)
			result[currentFolder] = subResult.result
			i = subResult.nextIndex
			continue
		} else if currentIndent < indentLevel {
			break
		}

		// 处理当前行
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}

		parts := strings.SplitN(trimmed, ":", 5)
		if len(parts) == 5 {
			// 文件: key:value1:value2:value3
			result[parts[0]] = []interface{}{parts[1], parts[2], strings.Join(parts[3:], ":")}
		} else if len(parts) == 4 {
			// 文件: key:value1:value2
			result[parts[0]] = []interface{}{parts[1], strings.Join(parts[2:], ":")}
		} else if len(parts) == 3 {
			// 文件: key:value
			result[parts[0]] = strings.Join(parts[1:], ":")
		} else if len(parts) == 2 {
			// 文件或目录: key:value
			if parts[1] == "" {
				// 目录
				currentFolder = parts[0]
				result[currentFolder] = make(map[string]interface{})
			} else {
				result[parts[0]] = parts[1]
			}
		} else {
			// 只有key的目录
			currentFolder = parts[0]
			result[currentFolder] = make(map[string]interface{})
		}

		i++
	}

	return parseResult{result: result, nextIndex: i}
}

// dict2Structure 将字典转换为Alist地址树结构
func (a2a *Ani2Alist) dict2Structure(dict map[string]interface{}) string {
	var builder strings.Builder
	a2a.dictToStructureRecursive(dict, 0, &builder)
	result := builder.String()

	// 移除开头的冒号和空格
	if strings.HasPrefix(result, ":") {
		result = strings.TrimLeft(result, ": ")
	}

	return result
}

func (a2a *Ani2Alist) dictToStructureRecursive(dict map[string]interface{}, indent int, builder *strings.Builder) {
	for key, value := range dict {
		switch v := value.(type) {
		case string:
			builder.WriteString(strings.Repeat(" ", indent))
			builder.WriteString(key)
			builder.WriteString(":")
			builder.WriteString(v)
			builder.WriteString("\n")
		case []interface{}:
			builder.WriteString(strings.Repeat(" ", indent))
			builder.WriteString(key)
			builder.WriteString(":")
			for i, item := range v {
				if i > 0 {
					builder.WriteString(":")
				}
				builder.WriteString(fmt.Sprintf("%v", item))
			}
			builder.WriteString("\n")
		case map[string]interface{}:
			builder.WriteString(strings.Repeat(" ", indent))
			builder.WriteString(key)
			builder.WriteString(":\n")
			a2a.dictToStructureRecursive(v, indent+2, builder)
		}
	}
}
