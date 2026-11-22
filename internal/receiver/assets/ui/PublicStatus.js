import { createElementVNode as _createElementVNode, toDisplayString as _toDisplayString, createTextVNode as _createTextVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, createCommentVNode as _createCommentVNode, Fragment as _Fragment, vModelText as _vModelText, withDirectives as _withDirectives, renderList as _renderList, vModelSelect as _vModelSelect, normalizeClass as _normalizeClass } from "./vue.js"

const _hoisted_1 = { class: "heading" }
const _hoisted_2 = ["disabled"]
const _hoisted_3 = {
  key: 0,
  class: "error",
  role: "alert"
}
const _hoisted_4 = { class: "panel" }
const _hoisted_5 = { class: "panel-head" }
const _hoisted_6 = {
  class: "last-update",
  "aria-live": "polite"
}
const _hoisted_7 = ["datetime"]
const _hoisted_8 = { class: "tools" }
const _hoisted_9 = {
  class: "scroll",
  role: "region",
  "aria-label": "Pillar status table",
  tabindex: "0"
}
const _hoisted_10 = { class: "status-table" }
const _hoisted_11 = {
  scope: "row",
  class: "pillar"
}
const _hoisted_12 = ["href"]
const _hoisted_13 = { "data-label": "Status" }
const _hoisted_14 = ["data-state"]
const _hoisted_15 = ["title"]
const _hoisted_16 = { "data-label": "Producer" }
const _hoisted_17 = ["title", "href"]
const _hoisted_18 = { key: 1 }
const _hoisted_19 = ["data-label", "title"]
const _hoisted_20 = {
  "data-label": "Resp. time",
  class: "mono",
  title: "Reporter’s local snapshot read time"
}
const _hoisted_21 = {
  "data-label": "Last checked (UTC)",
  class: "mono"
}
const _hoisted_22 = ["datetime"]
const _hoisted_23 = {
  key: 0,
  class: "empty",
  role: "status"
}
const _hoisted_24 = { key: 0 }

import { computed, onMounted, onUnmounted, ref } from './vue.js'


export default {
  __name: 'PublicStatus',
  setup(__props) {

const data = ref(null)
const failure = ref(false)
const loading = ref(false)
const query = ref('')
const filter = ref('All statuses')
const chains = ['BNB', 'ETH', 'Supernova']
const statuses = ['Online', 'Unavailable', 'Stale', 'Awaiting report']
const controller = new AbortController()
let timer

async function refresh() {
  if (loading.value) return
  loading.value = true
  try {
    const response = await fetch('/api/status', {
      cache: 'no-store',
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
    })
    if (!response.ok) throw new Error('Status unavailable')
    data.value = await response.json()
    failure.value = false
  } catch {
    if (!controller.signal.aborted) failure.value = true
  } finally {
    loading.value = false
  }
}

function clearFilters() {
  query.value = ''
  filter.value = 'All statuses'
}

function refreshVisible() {
  if (!document.hidden) refresh()
}
onMounted(() => {
  refresh()
  timer = setInterval(refreshVisible, 30000)
  document.addEventListener('visibilitychange', refreshVisible)
})
onUnmounted(() => {
  controller.abort()
  clearInterval(timer)
  document.removeEventListener('visibilitychange', refreshVisible)
})

const nodes = computed(() => data.value?.nodes ?? [])
const filtered = computed(() => {
  const term = query.value.trim().toLowerCase()
  return nodes.value.filter(
    (n) =>
      (filter.value === 'All statuses' || n.status === filter.value) &&
      (n.name.toLowerCase().includes(term) || n.producer?.includes(term)),
  )
})
const emptyMessage = computed(() => {
  if (!data.value) return failure.value ? 'Status unavailable' : 'Loading pillars…'
  return nodes.value.length ? 'No matching pillars' : 'No enrolled pillars yet'
})

const stateNames = {
  LiveState: 'Live',
  KeyGenState: 'Key generation',
  ReSignState: 'Re-signing',
  EmergencyState: 'Emergency',
  HaltedState: 'Halted',
  starting: 'Starting',
  waitingForZenon: 'Waiting for Zenon',
  syncingBridge: 'Syncing bridge',
  running: 'Running',
  stopping: 'Stopping',
}
const stamp = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'UTC',
  day: '2-digit',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})
const formatTime = (value) => stamp.format(new Date(value))
const number = (value) =>
  value?.value == null ? '—' : String(value.value) + (value.quality === 'partial' ? '*' : '')
function observation(label, obs) {
  return obs?.asOf
    ? `${label} · ${number(obs)} · ${obs.quality} · ${formatTime(obs.asOf)} UTC`
    : `${label} · unknown`
}
function queueTitle(pair) {
  return pair
    ? [observation('Unsigned wraps', pair.wrap), observation('Unsigned unwraps', pair.unwrap)].join(
        '\n',
      )
    : 'No observation'
}

return (_ctx, _cache) => {
  return (_openBlock(), _createElementBlock(_Fragment, null, [
    _createElementVNode("div", _hoisted_1, [
      _cache[3] || (_cache[3] = _createElementVNode("div", null, [
        _createElementVNode("h1", null, "Pillar status"),
        _createElementVNode("p", { class: "sub" }, "Health reports from enrolled bridge orchestrators.")
      ], -1 /* CACHED */)),
      _createElementVNode("button", {
        class: "quiet refresh",
        disabled: loading.value,
        onClick: refresh
      }, [
        _cache[2] || (_cache[2] = _createElementVNode("span", { "aria-hidden": "true" }, "↻", -1 /* CACHED */)),
        _createTextVNode(" " + _toDisplayString(loading.value ? 'Refreshing…' : 'Refresh'), 1 /* TEXT */)
      ], 8 /* PROPS */, _hoisted_2)
    ]),
    (failure.value)
      ? (_openBlock(), _createElementBlock("p", _hoisted_3, " Status refresh failed. " + _toDisplayString(data.value ? 'Showing the last received update.' : 'Try refreshing again.'), 1 /* TEXT */))
      : _createCommentVNode("v-if", true),
    _createElementVNode("section", _hoisted_4, [
      _createElementVNode("div", _hoisted_5, [
        _createElementVNode("span", _hoisted_6, [
          (data.value)
            ? (_openBlock(), _createElementBlock(_Fragment, { key: 0 }, [
                _cache[4] || (_cache[4] = _createTextVNode("Last update ", -1 /* CACHED */)),
                _createElementVNode("time", {
                  datetime: data.value.updated
                }, _toDisplayString(formatTime(data.value.updated)) + " UTC", 9 /* TEXT, PROPS */, _hoisted_7)
              ], 64 /* STABLE_FRAGMENT */))
            : (_openBlock(), _createElementBlock(_Fragment, { key: 1 }, [
                _createTextVNode("Waiting for status")
              ], 64 /* STABLE_FRAGMENT */))
        ]),
        _createElementVNode("div", _hoisted_8, [
          _withDirectives(_createElementVNode("input", {
            "onUpdate:modelValue": _cache[0] || (_cache[0] = $event => ((query).value = $event)),
            type: "search",
            "aria-label": "Find a pillar or producer",
            placeholder: "Find a pillar or producer"
          }, null, 512 /* NEED_PATCH */), [
            [_vModelText, query.value]
          ]),
          _withDirectives(_createElementVNode("select", {
            "onUpdate:modelValue": _cache[1] || (_cache[1] = $event => ((filter).value = $event)),
            "aria-label": "Filter by status"
          }, [
            _cache[5] || (_cache[5] = _createElementVNode("option", null, "All statuses", -1 /* CACHED */)),
            (_openBlock(), _createElementBlock(_Fragment, null, _renderList(statuses, (status) => {
              return _createElementVNode("option", { key: status }, _toDisplayString(status), 1 /* TEXT */)
            }), 64 /* STABLE_FRAGMENT */))
          ], 512 /* NEED_PATCH */), [
            [_vModelSelect, filter.value]
          ])
        ])
      ]),
      _createElementVNode("div", _hoisted_9, [
        _createElementVNode("table", _hoisted_10, [
          _createElementVNode("thead", null, [
            _createElementVNode("tr", null, [
              _cache[6] || (_cache[6] = _createElementVNode("th", { scope: "col" }, "Pillar", -1 /* CACHED */)),
              _cache[7] || (_cache[7] = _createElementVNode("th", { scope: "col" }, "Status", -1 /* CACHED */)),
              _cache[8] || (_cache[8] = _createElementVNode("th", { scope: "col" }, "State", -1 /* CACHED */)),
              _cache[9] || (_cache[9] = _createElementVNode("th", { scope: "col" }, "Producer", -1 /* CACHED */)),
              (_openBlock(), _createElementBlock(_Fragment, null, _renderList(chains, (chain) => {
                return _createElementVNode("th", {
                  key: chain,
                  scope: "col",
                  title: "Observed unsigned wraps / unwraps"
                }, _toDisplayString(chain), 1 /* TEXT */)
              }), 64 /* STABLE_FRAGMENT */)),
              _cache[10] || (_cache[10] = _createElementVNode("th", { scope: "col" }, "Resp. time", -1 /* CACHED */)),
              _cache[11] || (_cache[11] = _createElementVNode("th", { scope: "col" }, [
                _createTextVNode("Last checked "),
                _createElementVNode("small", null, "UTC")
              ], -1 /* CACHED */)),
              _cache[12] || (_cache[12] = _createElementVNode("th", { scope: "col" }, "Error", -1 /* CACHED */))
            ])
          ]),
          _createElementVNode("tbody", null, [
            (_openBlock(true), _createElementBlock(_Fragment, null, _renderList(filtered.value, (node) => {
              return (_openBlock(), _createElementBlock("tr", {
                key: node.name
              }, [
                _createElementVNode("th", _hoisted_11, [
                  (node.producer)
                    ? (_openBlock(), _createElementBlock("a", {
                        key: 0,
                        href: 'https://explorer.zenon.info/pillar/' + node.producer
                      }, _toDisplayString(node.name), 9 /* TEXT, PROPS */, _hoisted_12))
                    : (_openBlock(), _createElementBlock(_Fragment, { key: 1 }, [
                        _createTextVNode(_toDisplayString(node.name), 1 /* TEXT */)
                      ], 64 /* STABLE_FRAGMENT */))
                ]),
                _createElementVNode("td", _hoisted_13, [
                  _createElementVNode("span", {
                    class: "pill",
                    "data-state": node.status
                  }, _toDisplayString(node.status), 9 /* TEXT, PROPS */, _hoisted_14)
                ]),
                _createElementVNode("td", {
                  "data-label": "State",
                  title: stateNames[node.phase]
                }, _toDisplayString(stateNames[node.state] || stateNames[node.phase] || '—'), 9 /* TEXT, PROPS */, _hoisted_15),
                _createElementVNode("td", _hoisted_16, [
                  (node.producer)
                    ? (_openBlock(), _createElementBlock("a", {
                        key: 0,
                        class: "mono",
                        title: node.producer,
                        href: 'https://explorer.zenon.info/address/' + node.producer
                      }, _toDisplayString(node.producer.slice(0, 5)) + "…" + _toDisplayString(node.producer.slice(-5)), 9 /* TEXT, PROPS */, _hoisted_17))
                    : (_openBlock(), _createElementBlock("span", _hoisted_18, "—"))
                ]),
                (_openBlock(), _createElementBlock(_Fragment, null, _renderList(chains, (chain) => {
                  return _createElementVNode("td", {
                    key: chain,
                    "data-label": chain,
                    class: "mono",
                    title: queueTitle(node.queues?.[chain])
                  }, [
                    (node.queues?.[chain])
                      ? (_openBlock(), _createElementBlock(_Fragment, { key: 0 }, [
                          _createTextVNode(_toDisplayString(number(node.queues[chain].wrap)) + " / " + _toDisplayString(number(node.queues[chain].unwrap)), 1 /* TEXT */)
                        ], 64 /* STABLE_FRAGMENT */))
                      : (_openBlock(), _createElementBlock(_Fragment, { key: 1 }, [
                          _createTextVNode("—")
                        ], 64 /* STABLE_FRAGMENT */))
                  ], 8 /* PROPS */, _hoisted_19)
                }), 64 /* STABLE_FRAGMENT */)),
                _createElementVNode("td", _hoisted_20, _toDisplayString(node.readMicros == null
                  ? '—'
                  : (node.readMicros / 1000).toLocaleString(undefined, {
                      maximumFractionDigits: 2,
                    }) + ' ms'), 1 /* TEXT */),
                _createElementVNode("td", _hoisted_21, [
                  (node.checked)
                    ? (_openBlock(), _createElementBlock("time", {
                        key: 0,
                        datetime: node.checked
                      }, _toDisplayString(formatTime(node.checked)), 9 /* TEXT, PROPS */, _hoisted_22))
                    : (_openBlock(), _createElementBlock(_Fragment, { key: 1 }, [
                        _createTextVNode("—")
                      ], 64 /* STABLE_FRAGMENT */))
                ]),
                _createElementVNode("td", {
                  "data-label": "Error",
                  class: _normalizeClass({ issue: node.error })
                }, _toDisplayString(node.error || '—'), 3 /* TEXT, CLASS */)
              ]))
            }), 128 /* KEYED_FRAGMENT */))
          ])
        ])
      ]),
      (!filtered.value.length)
        ? (_openBlock(), _createElementBlock("div", _hoisted_23, [
            _createElementVNode("h3", null, _toDisplayString(emptyMessage.value), 1 /* TEXT */),
            (data.value && !nodes.value.length)
              ? (_openBlock(), _createElementBlock("p", _hoisted_24, "Pillars appear here when their reporter enrolls."))
              : _createCommentVNode("v-if", true),
            (data.value && nodes.value.length)
              ? (_openBlock(), _createElementBlock("button", {
                  key: 1,
                  class: "quiet",
                  onClick: clearFilters
                }, " Clear filters "))
              : _createCommentVNode("v-if", true)
          ]))
        : _createCommentVNode("v-if", true)
    ]),
    _cache[13] || (_cache[13] = _createElementVNode("details", { class: "guide" }, [
      _createElementVNode("summary", null, "How to read this table"),
      _createElementVNode("p", null, [
        _createElementVNode("b", null, "Online"),
        _createTextVNode(" means a recent heartbeat contained a valid local snapshot. "),
        _createElementVNode("b", null, "Unavailable"),
        _createTextVNode(" means the reporter checked in with a socket error. A heartbeat becomes "),
        _createElementVNode("b", null, "stale"),
        _createTextVNode(" after three minutes. ")
      ]),
      _createElementVNode("p", null, " Network columns show observed unsigned wraps / unwraps. An asterisk marks a partial count. A dash means the measurement is unavailable. Hover over a pair for its observation times. "),
      _createElementVNode("p", null, " Response time measures a successful snapshot read on the operator’s host. Last checked is the receiver’s heartbeat receipt time in UTC. This page refreshes every 30 seconds. ")
    ], -1 /* CACHED */))
  ], 64 /* STABLE_FRAGMENT */))
}
}

}