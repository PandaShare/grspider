package main

import (
    "bufio"
    "context"
    "fmt"
    "github.com/chromedp/chromedp/device"
    "grspider"
    "log"
    "os"
    "strings"
    "sync"
    "time"
)

var TotalCount = 0
var CompCount = 0
var errCnt = map[string]int64{}
var errCntLck sync.Mutex

func completed(obj *grspider.CrawlObject, result *grspider.CrawlResult) {
    log.Println(obj, result)
}

func main() {
    go func() {
        cur := 0
        for {
            if cur == 0 {
                time.Sleep(time.Second * 15)
                cur = CompCount
            } else {
                speed := float64(CompCount-cur) / float64(15)
                cur = CompCount
                fmt.Printf("Speed %.2f items/sec\n", speed)
                time.Sleep(time.Second * 15)
            }
        }
    }()
    balancer := grspider.NewEngine(context.Background(), 512)
    if err := balancer.AddSpider("http://172.17.0.2:9222", 16); err != nil {
        log.Fatal(err)
    } else if err := balancer.AddSpider("http://172.17.0.3:9222", 16); err != nil {
        log.Fatal(err)
    }
    balancer.Start()
    fd, _ := os.Open("input.txt")
    defer fd.Close()
    scanner := bufio.NewScanner(fd)
    for scanner.Scan() {
        TotalCount++
        host := strings.Trim(scanner.Text(), "\r\n\t ")
        if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
            host = fmt.Sprintf("http://%s", host)
        }
        miss := &grspider.CrawlObject{
            Host:             host,
            Timeout:          time.Second * 30,
            DialTimeout:      time.Second * 15,
            RenderTimeout:    time.Second * 5,
            EnableMedia:      true,
            EnableStylesheet: true,
            EnableScreenshot: true,
            Device: device.Info{
                "iPhone 11 Pro Max", "Mozilla/5.0 (iPhone; CPU iPhone OS 15_4_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.4 Mobile/15E148 Safari/604.1", 414, 896, 3.000000, false, true, true,
            },
            Completed: completed,
        }
        balancer.AddMission(miss)
    }
    time.Sleep(time.Hour)
}
