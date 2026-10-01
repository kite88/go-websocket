// 页面前端：主题三态、WebSocket 连接与自动重连、在线列表轮询、消息收发。
//
// 两条刻意的约定：
//   1. 所有用户可控文本都通过 jQuery 的 .text() 写进 DOM，绝不拼 HTML 字符串。
//      uid 与消息内容完全由对端控制，`<img src=x onerror=alert(1)>` 这种内容
//      一旦拼进 innerHTML 就是现成的 XSS。
//   2. 只用 Bootstrap 的 CSS，不用它的 JS 组件（内嵌的 bootstrap.min.js 是
//      「非 bundle 版」，不含 Popper），所以别引入下拉框 / tooltip / popover。

const THEME_KEY = 'gws-theme'
const UID_KEY = 'gws-uid'

// 重连退避：从 1s 开始翻倍，封顶 10s
const RECONNECT_MIN = 1000
const RECONNECT_MAX = 10000
// 在线列表轮询间隔
const ONLINE_POLL = 5000

const state = {
    uid: loadUid(),
    ws: null,
    connected: false,
    manualClose: false,
    retry: 0,
    online: [],
}

$(function () {
    initTheme()
    $('#my-uid').text(state.uid)
    bindEvents()
    connect()
    refreshOnline()
    setInterval(refreshOnline, ONLINE_POLL)
})

// ---------------------------------------------------------------- uid

// loadUid 取本地保存的 uid；没有就生成一个并存下来。
// 刷新页面后身份保持不变，别人还能接着给你发消息——旧版每次刷新都换 uid。
function loadUid() {
    try {
        const saved = localStorage.getItem(UID_KEY)
        if (saved) return saved
    } catch (e) {
        // 隐私模式下 localStorage 不可用，退化成「每次加载一个新 uid」
    }
    const uid = randomUid()
    saveUid(uid)
    return uid
}

function saveUid(uid) {
    try {
        localStorage.setItem(UID_KEY, uid)
    } catch (e) {
        // 存不下就算了，不影响本次会话
    }
}

// randomUid 生成形如 u-3f2a1b9c 的 uid：只用字母数字和连字符，
// 与服务端 common.ValidUID 的白名单保持一致。
function randomUid() {
    const bytes = new Uint8Array(4)
    crypto.getRandomValues(bytes)
    return 'u-' + Array.from(bytes, function (b) {
        return b.toString(16).padStart(2, '0')
    }).join('')
}

// ---------------------------------------------------------------- 主题（亮色 / 跟随系统 / 暗色）

function getTheme() {
    try {
        const saved = localStorage.getItem(THEME_KEY)
        if (saved === 'light' || saved === 'dark' || saved === 'system') return saved
    } catch (e) {
        // 忽略，按跟随系统处理
    }
    return 'system'
}

function isDarkTheme(mode) {
    if (mode === 'dark') return true
    if (mode === 'light') return false
    return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function applyTheme(mode) {
    document.documentElement.setAttribute('data-bs-theme', isDarkTheme(mode) ? 'dark' : 'light')
    $('.theme-switch button[data-theme]').each(function () {
        const active = $(this).attr('data-theme') === mode
        $(this).toggleClass('active', active).attr('aria-pressed', active)
    })
}

function initTheme() {
    applyTheme(getTheme())
    $('.theme-switch button[data-theme]').on('click', function () {
        const mode = $(this).attr('data-theme')
        try {
            localStorage.setItem(THEME_KEY, mode)
        } catch (e) {
            // 存不下也没关系，本次会话内仍然生效
        }
        applyTheme(mode)
    })
}

// ---------------------------------------------------------------- WebSocket

// wsURL 拼连接地址：协议、主机、端口全部跟着当前页面走。
// 旧版把 ws://127.0.0.1:8090 写死在前端，换端口、换机器、或者用 https 就全废了。
function wsURL(uid) {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = location.host || '127.0.0.1:8090' // 用 file:// 直接打开页面时的兜底
    return proto + '//' + host + '/ws?uid=' + encodeURIComponent(uid)
}

function connect() {
    state.manualClose = false
    setStatus('connecting')

    let ws
    try {
        ws = new WebSocket(wsURL(state.uid))
    } catch (e) {
        addSystem('无法创建 WebSocket 连接：' + e.message, true)
        scheduleReconnect()
        return
    }
    state.ws = ws

    ws.onopen = function () {
        state.retry = 0
        state.connected = true
        setStatus('online')
        addSystem('已连接，uid = ' + state.uid)
        refreshOnline()
    }

    ws.onmessage = function (e) {
        handleMessage(e.data)
    }

    ws.onerror = function () {
        // 浏览器不会给出错误细节，统一交给 onclose 处理重连
    }

    ws.onclose = function () {
        state.connected = false
        state.ws = null
        if (state.manualClose) {
            setStatus('closed')
            addSystem('连接已断开')
            return
        }
        setStatus('offline')
        scheduleReconnect()
    }
}

// scheduleReconnect 指数退避重连：重连风暴会把服务端刚重启的窗口期打满。
function scheduleReconnect() {
    const delay = Math.min(RECONNECT_MIN * Math.pow(2, state.retry), RECONNECT_MAX)
    state.retry++
    setTimeout(function () {
        if (!state.manualClose && !state.connected) connect()
    }, delay)
}

function disconnect() {
    state.manualClose = true
    if (state.ws) {
        state.ws.close()
        state.ws = null
    }
}

// handleMessage 处理服务端下行报文。两种类型：chat 与 error（见 common/protocol.go）。
function handleMessage(raw) {
    let msg
    try {
        msg = JSON.parse(raw)
    } catch (e) {
        addSystem('收到无法解析的报文：' + raw, true)
        return
    }

    if (msg.type === 'error') {
        addSystem(msg.content || '服务端返回了未知错误', true)
        return
    }

    addBubble({
        mine: false,
        who: msg.sender || '未知',
        time: msg.time || '',
        content: msg.content || '',
    })
    // 还没选接收人时，顺手把来信者设为回复对象
    if (!$('#receiver').val() && msg.sender) setReceiver(msg.sender)
}

function sendMessage() {
    const receiver = $.trim($('#receiver').val())
    const content = $.trim($('#content').val())

    if (!state.connected || !state.ws) {
        addSystem('尚未连接到服务端，消息未发送', true)
        return
    }
    if (!receiver) {
        addSystem('请先填写接收人 uid', true)
        return
    }
    if (!content) {
        addSystem('消息内容不能为空', true)
        return
    }

    state.ws.send(JSON.stringify({ receiver: receiver, content: content }))
    // 自己发的消息本地直接上屏：不这么做的话，要等服务端回显才看得到，
    // 而且服务端只投给接收方，根本不会回显给发送方。
    addBubble({ mine: true, who: '我', time: nowText(), content: content })
    setReceiver(receiver)
    $('#content').val('').focus()
}

// ---------------------------------------------------------------- 渲染

function setStatus(status) {
    const map = {
        connecting: ['连接中…', 'text-bg-secondary'],
        online: ['已连接', 'text-bg-success'],
        offline: ['已断开，重连中…', 'text-bg-danger'],
        closed: ['已关闭', 'text-bg-secondary'],
    }
    const item = map[status] || map.connecting
    $('#conn-status').text(item[0]).attr('class', 'badge ' + item[1])
}

function addBubble(opts) {
    $('#empty-hint').remove()
    const $bubble = $('<div>').addClass('bubble ' + (opts.mine ? 'mine' : 'them'))
    $('<span>').addClass('meta').text((opts.who || '') + ' · ' + (opts.time || '')).appendTo($bubble)
    $('<span>').text(opts.content || '').appendTo($bubble)
    appendToMessages($bubble)
}

function addSystem(text, isError) {
    $('#empty-hint').remove()
    const $line = $('<div>')
        .addClass('system-line' + (isError ? ' error' : ''))
        .text(text)
    appendToMessages($line)
}

function appendToMessages($el) {
    const $list = $('#messages')
    $list.append($el)
    $list.scrollTop($list[0].scrollHeight)
}

function renderOnline(users) {
    $('#online-count').text(users.length)
    const $list = $('#online-list').empty()

    if (!users.length) {
        $list.append($('<div>').addClass('text-secondary small p-3').text('当前没有其他在线用户'))
        return
    }

    users.forEach(function (uid) {
        const $item = $('<button>').attr('type', 'button').addClass('online-item').attr('data-uid', uid)
        $('<span>').addClass('uid').text(uid).appendTo($item)
        if (uid === state.uid) {
            $item.append($('<span>').addClass('badge text-bg-primary').text('我'))
        }
        if (uid === $('#receiver').val()) $item.addClass('active')
        $item.on('click', function () {
            setReceiver(uid)
        })
        $list.append($item)
    })
}

function setReceiver(uid) {
    $('#receiver').val(uid)
    $('#peer-name').text(uid || '（未选择）')
    $('.online-item').each(function () {
        $(this).toggleClass('active', $(this).attr('data-uid') === uid)
    })
}

function refreshOnline() {
    $.getJSON('/api/online')
        .done(function (res) {
            state.online = (res && res.users) || []
            renderOnline(state.online)
        })
        .fail(function () {
            // 轮询失败不打扰用户：界面上只体现为人数不再刷新
        })
}

// ---------------------------------------------------------------- 交互绑定

function bindEvents() {
    $('#composer').on('submit', function (e) {
        e.preventDefault()
        sendMessage()
    })

    $('#receiver').on('input', function () {
        setReceiver($.trim($(this).val()))
    })

    $('#copy-uid').on('click', function () {
        copyText(state.uid, $(this))
    })

    $('#change-uid').on('click', function () {
        disconnect()
        state.uid = randomUid()
        saveUid(state.uid)
        $('#my-uid').text(state.uid)
        $('#receiver').val('')
        $('#peer-name').text('（未选择）')
        state.retry = 0
        connect()
    })
}

// copyText 复制文本，成功后把链接短暂变成「已复制」。
// navigator.clipboard 只在安全上下文（https / localhost）可用，其余场景退回 execCommand。
function copyText(text, $link) {
    const done = function () {
        const original = $link.text()
        $link.addClass('copied').text('已复制')
        setTimeout(function () {
            $link.removeClass('copied').text(original)
        }, 1200)
    }
    const fallback = function () {
        const $tmp = $('<textarea>').val(text).css({ position: 'fixed', top: '-1000px' }).appendTo(document.body)
        $tmp[0].select()
        try {
            document.execCommand('copy')
            done()
        } catch (e) {
            // 复制失败就保持原样，用户还能手动选中
        }
        $tmp.remove()
    }

    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(done, fallback)
        return
    }
    fallback()
}

function nowText() {
    const d = new Date()
    return [d.getHours(), d.getMinutes(), d.getSeconds()].map(function (n) {
        return String(n).padStart(2, '0')
    }).join(':')
}
