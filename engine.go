package grspider

import (
    "context"
    "errors"
    "sync"
)

var (
    ErrIsRunning = errors.New("engine is running")
    ErrIsStopped = errors.New("engine is stopped")
)

type Engine struct {
    lck     sync.RWMutex
    c       context.Context
    cancel  context.CancelFunc
    running bool
    channel chan *CrawlObject
    spider  []*RenderSpider
}

func NewEngine(c context.Context, cacheSize int64) *Engine {
    ctx, cancel := context.WithCancel(c)
    return &Engine{
        lck:     sync.RWMutex{},
        c:       ctx,
        cancel:  cancel,
        running: false,
        channel: make(chan *CrawlObject, cacheSize),
        spider:  []*RenderSpider{},
    }
}

func (b *Engine) AddSpider(addr string, maxTab int) error {
    b.lck.Lock()
    defer b.lck.Unlock()
    if b.running {
        return ErrIsRunning
    } else if sp, err := NewRenderSpider(b.c, addr, maxTab, b.channel); err != nil {
        return err
    } else {
        b.spider = append(b.spider, sp)
        return nil
    }
}

func (b *Engine) Start() {
    b.lck.Lock()
    defer b.lck.Unlock()
    b.running = true
}

func (b *Engine) Close() {
    b.lck.Lock()
    defer b.lck.Unlock()
    if b.running == false {
        // 已经关闭或者未启动则不许重复关闭
        return
    }
    b.running = false
    b.cancel()
    close(b.channel)
}

func (b *Engine) AddMission(object *CrawlObject) error {
    b.lck.RLock()
    defer b.lck.RUnlock()
    if b.running == false {
        return ErrIsStopped
    }
    b.channel <- object
    return nil
}
