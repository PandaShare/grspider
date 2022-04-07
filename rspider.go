package grspider

import (
    "context"
    _ "embed"
    "encoding/json"
    "fmt"
    "github.com/chromedp/cdproto/cdp"
    "github.com/chromedp/cdproto/emulation"
    "github.com/chromedp/cdproto/fetch"
    "github.com/chromedp/cdproto/network"
    "github.com/chromedp/cdproto/page"
    "github.com/chromedp/cdproto/runtime"
    "github.com/chromedp/cdproto/serviceworker"
    "github.com/chromedp/chromedp"
    "github.com/chromedp/chromedp/device"
    "math"
    "net/http"
    "strings"
    "sync"
    "time"
)

//go:embed env.js
var envJS []byte

//go:embed fetchData.js
var fetchDataJS []byte

type ErrStage string

const (
    ErrStageInit       ErrStage = "initialize" // 初始化爬虫阶段
    ErrStageConnecting ErrStage = "connecting" // 连接阶段错误
    ErrStageScreen     ErrStage = "screen"     // 截屏失败
    ErrStageFetchInit  ErrStage = "fetch:init" // 采集数据阶段，初始化失败
    ErrStageFetchData  ErrStage = "fetch:data" // 采集数据阶段，采集数据JS执行失败
)

type TimeConsumeStage string

const (
    TimeConsumeOnAll       = "all"
    TimeConsumeStartAt     = "start_at"
    TimeConsumeEndAt       = "end_at"
    TimeConsumeOnNavigate  = "navigate"
    TimeConsumeOnScreen    = "screen"
    TimeConsumeOnFetchData = "fetch"
)

const (
    ctxKeyFrameId2ExecutionId = "fte"
)

type fetchDataResult struct {
    Title   []string `json:"title"`
    Favicon string   `json:"favicon"`
    HTML    string   `json:"html"`
    Text    string   `json:"text"`
}

type RenderSpider struct {
    channel          <-chan *CrawlObject
    allocatorContext context.Context
    allocatorCancel  context.CancelFunc
}

type CrawlObject struct {
    Host             string                                   // 需要爬取的域名
    Timeout          time.Duration                            // 整个站点爬取的超时时间
    DialTimeout      time.Duration                            // 站点等待加载完成时间
    RenderTimeout    time.Duration                            // 等待动画时间
    EnableMedia      bool                                     // 是否允许多媒体传输
    EnableStylesheet bool                                     // 是否允许传输 CSS
    EnableScreenshot bool                                     // 是否需要截屏
    Device           device.Info                              // 模拟设备
    Completed        func(obj *CrawlObject, res *CrawlResult) // 完成的回调
}

type CrawlResourceObject struct {
    URL          string               `json:"url"`                    // Resource URL.
    Type         network.ResourceType `json:"type"`                   // Type of this resource. 这一段暂时没有封装
    MimeType     string               `json:"mimeType"`               // Resource mimeType as determined by the browser.
    LastModified time.Time            `json:"lastModified,omitempty"` // last-modified timestamp as reported by server.
    ContentSize  float64              `json:"contentSize,omitempty"`  // Resource content size.
    Failed       bool                 `json:"failed,omitempty"`       // True if the resource failed to load.
    Canceled     bool                 `json:"canceled,omitempty"`     // True if the resource was canceled during loading.
}

type CrawlResultFrameObject struct {
    Favicon     string                   `json:"favicon"`      // favicon 路径
    FaviconHash string                   `json:"favicon_hash"` // favicon sha256
    URL         string                   `json:"url"`          // 当前 frame 的 url
    Title       []string                 `json:"title"`        // 有可能出现多个标题的情况, 特别是黄站，或者多个frame嵌套的恶心操作
    Text        string                   `json:"text"`         // 该 frame 的文本
    Html        string                   `json:"html"`         // 该 frame 的 html
    Resources   []CrawlResourceObject    `json:"resources"`    // 该 frame 调用的资源
    Child       []CrawlResultFrameObject `json:"child"`        // 该 frame 节点的子结果
}

type CrawlResult struct {
    SentRequests        []*network.EventRequestWillBeSent                              `json:"sent_requests"`
    RequestWithResponse []*network.EventResponseReceived                               `json:"request_with_response"`
    RequestResponseMap  map[network.RequestID]*network.Response                        `json:"request_response_map"`
    RequestExtraMap     map[network.RequestID]*network.EventRequestWillBeSentExtraInfo `json:"request_extra_map"`
    ResponseExtraMap    map[network.RequestID]*network.EventResponseReceivedExtraInfo  `json:"response_extra_map"`
    Screen              []byte                                                         `json:"screen"`       // 整站屏幕, 不再根据 frame 来截图了，太麻烦了
    Errors              map[ErrStage][]string                                          `json:"errors"`       // 阶段错误
    TimeConsume         map[TimeConsumeStage]int64                                     `json:"time_consume"` // 耗时， ms
    Root                CrawlResultFrameObject                                         `json:"root"`         // 采集结果
}

type deviceEmulateObject struct {
    Dev device.Info
}

func (d *deviceEmulateObject) Device() device.Info {
    return d.Dev
}

func resetRemote(addr string) {

}

func NewRenderSpider(c context.Context, addr string, maxTab int, missionChannel <-chan *CrawlObject) (*RenderSpider, error) {
    if resp, err := http.Get(fmt.Sprintf("%s/json/version", strings.Trim(addr, "/"))); err != nil {
        return nil, err
    } else {
        defer resp.Body.Close()
        type versionObject struct {
            Browser              string `json:"Browser"`
            ProtocolVersion      string `json:"Protocol-Version"`
            UserAgent            string `json:"User-Agent"`
            V8Version            string `json:"V8-Version"`
            WebKitVersion        string `json:"WebKit-Version"`
            WebSocketDebuggerUrl string `json:"webSocketDebuggerUrl"`
        }
        ver := versionObject{}
        if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
            return nil, err
        } else {
            ctx, cancel := chromedp.NewRemoteAllocator(c, ver.WebSocketDebuggerUrl)
            ret := &RenderSpider{
                channel:          missionChannel,
                allocatorContext: ctx,
                allocatorCancel:  cancel,
            }
            for i := 0; i < maxTab; i++ {
                go ret.asyncWorker()
            }
            return ret, nil
        }
    }
}

func (r *RenderSpider) asyncWorker() {
    for miss := range r.channel {
        miss.Completed(miss, r.crawl(miss))
    }
}
func (r *RenderSpider) Close() error {
    r.allocatorCancel()
    return nil
}

// screenShot 用于全屏幕的截屏，根据 ctx 自动切换高宽
func (r *RenderSpider) screenShot(ctx context.Context) ([]byte, error) {
    // 使用传统方式，动态调整分辨率实在太吃资源了
    return page.CaptureScreenshot().
        WithQuality(90).
        WithFromSurface(false).
        WithCaptureBeyondViewport(true).
        Do(ctx)
    // get layout metrics
    _, _, contentSize, _, _, _, err := page.GetLayoutMetrics().Do(ctx)
    if err != nil {
        return []byte{}, err
    }

    width, height := int64(math.Ceil(contentSize.Width)), int64(math.Ceil(contentSize.Height))

    // force viewport emulation
    err = emulation.SetDeviceMetricsOverride(width, height, 1, false).
        WithScreenOrientation(&emulation.ScreenOrientation{
            Type:  emulation.OrientationTypePortraitPrimary,
            Angle: 0,
        }).
        Do(ctx)
    if err != nil {
        return []byte{}, err
    }
    // capture screenshot
    return page.CaptureScreenshot().
        WithQuality(90).
        WithClip(&page.Viewport{
            X:      contentSize.X,
            Y:      contentSize.Y,
            Width:  contentSize.Width,
            Height: contentSize.Height,
            Scale:  1,
        }).Do(ctx)
}

func (r *RenderSpider) buildResultFromFrame(
    target *CrawlObject,
    taskCtx context.Context,
    result *CrawlResult, //  改变量只用于填写 ErrStage 的问题
    frameResult *CrawlResultFrameObject,
    curFrame *cdp.Frame,
    curFrameResource []*page.FrameResource,
    childs []*page.FrameResourceTree,
) {
    fte := taskCtx.Value(ctxKeyFrameId2ExecutionId).(map[cdp.FrameID]runtime.ExecutionContextID)
    frameResult.URL = curFrame.URL
    chromedp.Run(taskCtx, chromedp.Tasks{
        chromedp.ActionFunc(func(ctx context.Context) error {
            // 采集数据
            data := fetchDataResult{}
            if err := chromedp.Evaluate(string(fetchDataJS), &data, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
                return p.WithAwaitPromise(true).WithContextID(fte[curFrame.ID])
            }).Do(ctx); err != nil {
                if _, ok := result.Errors[ErrStageFetchData]; !ok {
                    result.Errors[ErrStageFetchData] = []string{}
                }
                result.Errors[ErrStageFetchData] = append(result.Errors[ErrStageFetchData], fmt.Sprintf("chromedp.Evaluate() error due to %v", err))
            } else {
                frameResult.Favicon = data.Favicon
                frameResult.Title = data.Title
                frameResult.Text = data.Text
                frameResult.Html = data.HTML
            }
            return nil
        }),
    })
    // 将对应的 resource 资源写进去
    for _, res := range curFrameResource {
        frameResult.Resources = append(frameResult.Resources, CrawlResourceObject{
            URL:      res.URL,
            Type:     res.Type,
            MimeType: res.MimeType,
            LastModified: func() time.Time {
                if res.LastModified == nil {
                    return time.Unix(0, 0)
                } else {
                    return res.LastModified.Time()
                }
            }(),
            ContentSize: res.ContentSize,
            Failed:      res.Failed,
            Canceled:    res.Canceled,
        })
    }
    // 分配足够的资源给 child
    frameResult.Child = make([]CrawlResultFrameObject, len(childs))
    // 开始获取所有子元素的结果
    for i := range childs {
        r.buildResultFromFrame(
            target,
            taskCtx,
            result,
            &frameResult.Child[i],
            childs[i].Frame,
            childs[i].Resources,
            childs[i].ChildFrames,
        )
    }
}

// buildResult 构造结果
func (r *RenderSpider) buildResult(target *CrawlObject, taskCtx context.Context, result *CrawlResult) CrawlResultFrameObject {
    // 获取整个站的返回结果
    root := CrawlResultFrameObject{
        Title:     []string{},
        Text:      "",
        Html:      "",
        Resources: []CrawlResourceObject{},
    }
    isErr := false
    var tree *page.FrameResourceTree = nil
    chromedp.Run(taskCtx, chromedp.Tasks{
        chromedp.ActionFunc(func(ctx context.Context) error {
            if fRes, err := page.GetResourceTree().Do(ctx); err != nil {
                result.Errors[ErrStageFetchInit] = []string{err.Error()}
                isErr = true
            } else {
                isErr = false
                tree = fRes
            }
            return nil
        }),
    })
    if !isErr {
        // 这里要修正 context, 将 context 修复为 frame 自身的 context
        r.buildResultFromFrame(target, taskCtx, result, &root, tree.Frame, tree.Resources, tree.ChildFrames)
    }
    return root
}

func (r *RenderSpider) crawl(target *CrawlObject) *CrawlResult {
    startAt := time.Now()
    // 创建结果
    result := &CrawlResult{
        SentRequests:        []*network.EventRequestWillBeSent{},
        RequestWithResponse: []*network.EventResponseReceived{},
        RequestResponseMap:  map[network.RequestID]*network.Response{},
        RequestExtraMap:     map[network.RequestID]*network.EventRequestWillBeSentExtraInfo{},
        ResponseExtraMap:    map[network.RequestID]*network.EventResponseReceivedExtraInfo{},
        TimeConsume: map[TimeConsumeStage]int64{
            TimeConsumeStartAt:     startAt.UnixMilli(),
            TimeConsumeEndAt:       0,
            TimeConsumeOnAll:       0,
            TimeConsumeOnNavigate:  0,
            TimeConsumeOnScreen:    0,
            TimeConsumeOnFetchData: 0,
        },
        Errors: map[ErrStage][]string{},
    }
    defer func() {
        endAt := time.Now()
        result.TimeConsume[TimeConsumeEndAt] = endAt.UnixMilli()
        result.TimeConsume[TimeConsumeOnAll] = endAt.Sub(startAt).Milliseconds()
    }()
    // 创建任务的 context
    taskCtx, taskCancel := chromedp.NewContext(r.allocatorContext)
    defer taskCancel()

    // 确保浏览器已经运行
    if err := chromedp.Run(taskCtx); err != nil {
        result.Errors[ErrStageInit] = []string{err.Error()}
        return result
    }
    // 创建基于整站的超时
    taskCtx, taskCancel = context.WithTimeout(taskCtx, target.Timeout)
    defer taskCancel()
    // 记录 frameId 和 executionId 的关联
    frameToExecutionIdMap := map[cdp.FrameID]runtime.ExecutionContextID{}
    fteLck := sync.Mutex{}

    // 创建监听器，用于过滤传输
    chromedp.ListenTarget(taskCtx, func(event interface{}) {
        switch ev := event.(type) {
        // 这里要处理 iframe 的一些数据记录
        case *runtime.EventExecutionContextsCleared:
            fteLck.Lock()
            frameToExecutionIdMap = map[cdp.FrameID]runtime.ExecutionContextID{}
            fteLck.Unlock()
        case *runtime.EventExecutionContextDestroyed:
            fteLck.Lock()
            for frameID, ctxID := range frameToExecutionIdMap {
                if ctxID == ev.ExecutionContextID {
                    delete(frameToExecutionIdMap, frameID)
                }
            }
            fteLck.Unlock()
        case *runtime.EventExecutionContextCreated:
            var aux struct {
                FrameID cdp.FrameID
            }
            if len(ev.Context.AuxData) == 0 {
                break
            }
            if err := json.Unmarshal(ev.Context.AuxData, &aux); err != nil {
                break
            }
            if aux.FrameID != "" {
                fteLck.Lock()
                frameToExecutionIdMap[aux.FrameID] = ev.Context.ID
                fteLck.Unlock()
            }
        case *fetch.EventAuthRequired:
            // 有一些弹窗提示要求鉴权的情况，这里过滤掉
            go func() {
                chromedp.Run(taskCtx, fetch.FailRequest(ev.RequestID, network.ErrorReasonAccessDenied))
            }()
        case *page.EventJavascriptDialogOpening:
            // 有些 alert confirm 的弹窗问题
            go func() {
                chromedp.Run(taskCtx, page.HandleJavaScriptDialog(false))
            }()
        case *network.EventRequestWillBeSentExtraInfo:
            result.RequestExtraMap[ev.RequestID] = ev
        case *network.EventResponseReceivedExtraInfo:
            result.ResponseExtraMap[ev.RequestID] = ev
        case *network.EventResponseReceived:
            // 记录所有 request 的 response
            result.RequestResponseMap[ev.RequestID] = ev.Response
            result.RequestWithResponse = append(result.RequestWithResponse, ev)
        case *network.EventRequestWillBeSent:
            // 记录即将发送的 request
            result.SentRequests = append(result.SentRequests, ev)
        case *fetch.EventRequestPaused:
            // 有一些资源需过滤，不获取，减少宽带开销
            go func() {
                c := chromedp.FromContext(taskCtx)
                ctx := cdp.WithExecutor(taskCtx, c.Target)
                switch ev.ResourceType {
                case network.ResourceTypeFont:
                    fallthrough
                case network.ResourceTypeImage:
                    fallthrough
                case network.ResourceTypeMedia:
                    if target.EnableMedia {
                        fetch.ContinueRequest(ev.RequestID).Do(ctx)
                    } else {
                        fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
                    }
                case network.ResourceTypeStylesheet:
                    if target.EnableStylesheet {
                        fetch.ContinueRequest(ev.RequestID).Do(ctx)
                    } else {
                        fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
                    }
                case network.ResourceTypeWebSocket:
                    fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
                default:
                    fetch.ContinueRequest(ev.RequestID).Do(ctx)
                }
            }()
        }
    })
    // 开始一个爬虫任务
    deviceEmulate := &deviceEmulateObject{Dev: target.Device}
    // 创建一个基于 dial 超时的任务
    dialCtx, dialCancel := context.WithTimeout(taskCtx, target.DialTimeout)
    defer dialCancel()
    // 开始访问
    consumeStart := time.Now()
    _, err := chromedp.RunResponse(
        dialCtx,
        // 开启 fetch 过滤
        fetch.Enable(),
        chromedp.Tasks{
            // 关闭不要的一些东西
            chromedp.ActionFunc(func(ctx context.Context) error {
                serviceworker.Disable().Do(ctx)
                return nil
            }),
            // 修正环境
            chromedp.ActionFunc(func(ctx context.Context) error {
                _, err := page.AddScriptToEvaluateOnNewDocument(string(envJS)).Do(ctx)
                return err
            }),
        },
        // 模拟机型
        chromedp.Emulate(deviceEmulate),
        // 访问 URL
        chromedp.Navigate(target.Host),
    )
    result.TimeConsume[TimeConsumeOnNavigate] = time.Now().Sub(consumeStart).Milliseconds()
    if err != nil && err != context.DeadlineExceeded {
        // 如果出错， 并且错误原因并不是超时，则是 chrome 内部错误，直接返回
        result.Errors[ErrStageConnecting] = []string{err.Error()}
        return result
    }
    /**
      这里进行渲染等待， 虽然 chromedp 推荐是使用 chromedp.Run(chromedp.Sleep) 进行休眠

      但是测试中发现，这样搞会卡住整个浏览器，暂时没找到原因

      也可能是 demo 测试写错了导致的

      所以这里直接暂停该 goroutine 吧，这样也好拆开 context 超时的问题
    */
    time.Sleep(target.RenderTimeout)
    // 截图
    consumeStart = time.Now()
    chromedp.Run(taskCtx, chromedp.Tasks{
        chromedp.ActionFunc(func(ctx context.Context) error {
            // 获取截屏
            if target.EnableScreenshot {
                if screen, err := r.screenShot(ctx); err != nil {
                    result.Errors[ErrStageScreen] = []string{err.Error()}
                } else {
                    result.Screen = screen
                }
            }
            return nil
        }),
    })
    result.TimeConsume[TimeConsumeOnScreen] = time.Now().Sub(consumeStart).Milliseconds()
    // 停止加载
    chromedp.Run(taskCtx, chromedp.Stop())
    // 获取所有资源
    consumeStart = time.Now()
    result.Root = r.buildResult(target, context.WithValue(taskCtx, ctxKeyFrameId2ExecutionId, frameToExecutionIdMap), result)
    result.TimeConsume[TimeConsumeOnFetchData] = time.Now().Sub(consumeStart).Milliseconds()
    return result
}
