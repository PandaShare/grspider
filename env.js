
// 参考 https://www.buaq.net/go-86083.html
// =============== 防止 headless 检测
// Pass the Webdriver Test.
Object.defineProperty(navigator, 'webdriver', {
    get: () => false,
});
// Pass the Plugins Length Test.
// Overwrite the plugins property to use a custom getter.
Object.defineProperty(navigator, 'plugins', {
    // This just needs to have length > 0 for the current test,
    // but we could mock the plugins too if necessary.
    get: () => [1, 2, 3, 4, 5],
});

// Pass the Chrome Test.
// We can mock this in as much depth as we need for the test.
window.chrome = {
    runtime: {},
};

// Pass the Permissions Test.
const originalQuery = window.navigator.permissions.query;
window.navigator.permissions.query = (parameters) => (
    parameters.name === 'notifications' ?
        Promise.resolve({ state: Notification.permission }) :
        originalQuery(parameters)
);

//Pass the Permissions Test. navigator.userAgent
Object.defineProperty(navigator, 'userAgent', {
    get: () => "Mozilla/5.0 (iPhone; CPU iPhone OS 15_4_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.4 Mobile/15E148 Safari/604.1",
});

// 修改浏览器对象的属性
Object.defineProperty(navigator, 'platform', {
    get: function () { return 'iPhone'; }
});

Object.defineProperty(navigator, 'language', {
    get: function () { return 'zh-CN'; }
});

Object.defineProperty(navigator, 'languages', {
    get: function () { return ["zh-CN", "zh"]; }
});

// ============== 时间加速

// hook setTimeout
window.__originalSetTimeout = window.setTimeout;
window.setTimeout = function () {
    arguments[1] = arguments[1] / 3;
    return window.__originalSetTimeout.apply(this, arguments);
};
Object.defineProperty(window, "setTimeout", {"writable": false, "configurable": false});

// hook setInterval
window.__originalSetInterval = window.setInterval;
window.setInterval = function () {
    arguments[1] =  arguments[1] / 3;
    return window.__originalSetInterval.apply(this, arguments);
};
Object.defineProperty(window, "setInterval", {"writable": false, "configurable": false});




// =============== 由于时间加速，防止 ajax 过度调用

// 劫持原生ajax，并对每个请求设置最大请求次数
window.ajax_req_count_sec_auto = {};
XMLHttpRequest.prototype.__originalOpen = XMLHttpRequest.prototype.open;
XMLHttpRequest.prototype.open = function(method, url, async, user, password) {
    // hook code
    this.url = url;
    this.method = method;
    let name = method + url;
    if (!window.ajax_req_count_sec_auto.hasOwnProperty(name)) {
        window.ajax_req_count_sec_auto[name] = 1
    } else {
        window.ajax_req_count_sec_auto[name] += 1
    }

    if (window.ajax_req_count_sec_auto[name] <= 3) {
        return this.__originalOpen(method, url, true, user, password);
    }
}
Object.defineProperty(XMLHttpRequest.prototype,"open",{"writable": false, "configurable": false});

XMLHttpRequest.prototype.__originalSend = XMLHttpRequest.prototype.send;
XMLHttpRequest.prototype.send = function(data) {
    // hook code
    let name = this.method + this.url;
    if (window.ajax_req_count_sec_auto[name] <= 10) {
        return this.__originalSend(data);
    }
}
Object.defineProperty(XMLHttpRequest.prototype,"send",{"writable": false, "configurable": false});
XMLHttpRequest.prototype.__originalAbort = XMLHttpRequest.prototype.abort;
XMLHttpRequest.prototype.abort = function() {
    // hook code
}
Object.defineProperty(XMLHttpRequest.prototype,"abort",{"writable": false, "configurable": false});

window.__originFetch = window.fetch;
window.fetch = function () {
    opts = arguments[1]
    const method = opts.method || 'GET';
    const name = method + arguments[0];
    if (!window.ajax_req_count_sec_auto.hasOwnProperty(name)) {
        window.ajax_req_count_sec_auto[name] = 1
    } else {
        window.ajax_req_count_sec_auto[name] += 1
    }

    if (window.ajax_req_count_sec_auto[name] <= 3) {
        return this.__originFetch(arguments[0], arguments[1])
    } else {
        return new Promise((resolve, reject) => {
            reject()
        })
    }
};
Object.defineProperty(window, "fetch", {"writable": false, "configurable": false});

