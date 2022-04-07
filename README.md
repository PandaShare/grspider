# 说明

grspider 是基于 chrome headless 的爬虫

用于解决无法通过常规爬虫采集基于 react/vue 之类的网站


# headless 说明

headless 内的 dockerfile 是基于 chromedp 官方的 headless docker image 进一步自定义的产物

主要是为了解决中文乱码以及时区的问题

使用方法

```shell
docker build -t headless:latest .
docker save headless:latest|gzip > headless.tgz
scp headless.tgz root@domain.com:~/
# 切换到远程服务器
gunzip -c headless.tgz |docker load
docker run -dit --restart=always --name headless -p 0.0.0.0:9222:9222 --shm-size 8G headless:latest
```


# PS

该项目只是实验性质项目，不确定潜在bug

