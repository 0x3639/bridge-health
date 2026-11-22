import { createElementVNode as _createElementVNode, toDisplayString as _toDisplayString, openBlock as _openBlock, createElementBlock as _createElementBlock, createCommentVNode as _createCommentVNode, createTextVNode as _createTextVNode, renderList as _renderList, Fragment as _Fragment, unref as _unref } from "./vue.js"

const _hoisted_1 = {
  key: 0,
  class: "admin-nav",
  "aria-label": "Administration"
}
const _hoisted_2 = ["aria-current"]
const _hoisted_3 = ["aria-current"]
const _hoisted_4 = ["aria-current"]
const _hoisted_5 = {
  method: "post",
  action: "/logout"
}
const _hoisted_6 = {
  key: 1,
  role: "alert",
  class: "error"
}
const _hoisted_7 = {
  key: 2,
  class: "panel access"
}
const _hoisted_8 = {
  key: 3,
  class: "panel access"
}
const _hoisted_9 = {
  key: 0,
  class: "stack"
}
const _hoisted_10 = ["value"]
const _hoisted_11 = {
  key: 1,
  class: "error"
}
const _hoisted_12 = {
  class: "metrics",
  "aria-label": "Fleet summary"
}
const _hoisted_13 = { class: "metric-label" }
const _hoisted_14 = { class: "panel" }
const _hoisted_15 = {
  key: 0,
  class: "scroll"
}
const _hoisted_16 = ["href"]
const _hoisted_17 = ["data-state"]
const _hoisted_18 = { class: "mono" }
const _hoisted_19 = {
  key: 1,
  class: "empty"
}
const _hoisted_20 = {
  key: 0,
  class: "panel create notice"
}
const _hoisted_21 = { class: "inline" }
const _hoisted_22 = ["value"]
const _hoisted_23 = { class: "panel" }
const _hoisted_24 = {
  key: 0,
  class: "scroll"
}
const _hoisted_25 = { class: "mono" }
const _hoisted_26 = ["action", "onSubmit"]
const _hoisted_27 = {
  key: 1,
  class: "empty"
}
const _hoisted_28 = {
  key: 0,
  class: "panel create notice"
}
const _hoisted_29 = { class: "inline" }
const _hoisted_30 = ["value"]
const _hoisted_31 = { class: "panel" }
const _hoisted_32 = {
  key: 0,
  class: "scroll"
}
const _hoisted_33 = ["href"]
const _hoisted_34 = ["data-state"]
const _hoisted_35 = { class: "mono" }
const _hoisted_36 = ["action", "onSubmit"]
const _hoisted_37 = { class: "quiet" }
const _hoisted_38 = { key: 1 }
const _hoisted_39 = {
  key: 1,
  class: "empty"
}
const _hoisted_40 = { class: "heading" }
const _hoisted_41 = ["data-state"]
const _hoisted_42 = { class: "panel create" }
const _hoisted_43 = ["action"]
const _hoisted_44 = {
  key: 0,
  class: "panel create"
}
const _hoisted_45 = ["action"]
const _hoisted_46 = ["value"]
const _hoisted_47 = {
  key: 1,
  class: "panel create"
}
const _hoisted_48 = {
  role: "status",
  class: "copy-status"
}

import { computed, ref } from './vue.js'


export default {
  __name: 'Management',
  props: { page: Object },
  setup(__props) {


const detail = __props.page.Detail
const copied = ref('')
const shareField = ref(null)
const commandField = ref(null)
const token = location.hash.slice(1)
const installCommand = computed(() => {
  if (__props.page.View !== 'install' || !/^[a-f0-9-]{36}\.[A-Za-z0-9_-]{43}$/.test(token)) return ''
  return `installer=$(curl -fsS '${__props.page.Origin}/install.py') && printf '%s\\n' '${token}' | sudo python3 -I -c "$installer"`
})
const stamp = (value) =>
  !value || value.startsWith('0001-')
    ? '—'
    : new Intl.DateTimeFormat('en-GB', {
        timeZone: 'UTC',
        dateStyle: 'medium',
        timeStyle: 'short',
      }).format(new Date(value)) + ' UTC'
async function copy(field) {
  try {
    await navigator.clipboard.writeText(field.value)
    copied.value = 'Copied to clipboard.'
  } catch {
    field.focus()
    field.select()
    copied.value = 'Selected for copying.'
  }
}
function confirmTokenRevoke(event, token) {
  if (!window.confirm(`Revoke the API token for ${token.Name}? Its access will end immediately.`))
    event.preventDefault()
}
function confirmRevoke(event, node) {
  const message = node.Enrolled
    ? `Revoke access for ${node.Name}? The receiver will reject future heartbeats.`
    : `Cancel the invitation for ${node.Name}?`
  if (!window.confirm(message)) event.preventDefault()
}

return (_ctx, _cache) => {
  return (_openBlock(), _createElementBlock(_Fragment, null, [
    (__props.page.User)
      ? (_openBlock(), _createElementBlock("nav", _hoisted_1, [
          _createElementVNode("a", {
            href: "/admin",
            "aria-current": ['overview', 'detail'].includes(__props.page.View) ? 'page' : null
          }, "Overview", 8 /* PROPS */, _hoisted_2),
          _createElementVNode("a", {
            href: "/invitations",
            "aria-current": __props.page.View === 'invites' ? 'page' : null
          }, "Invitations", 8 /* PROPS */, _hoisted_3),
          _createElementVNode("a", {
            href: "/admin/tokens",
            "aria-current": __props.page.View === 'tokens' ? 'page' : null
          }, "API tokens", 8 /* PROPS */, _hoisted_4),
          _createElementVNode("form", _hoisted_5, [
            _createElementVNode("span", null, _toDisplayString(__props.page.User), 1 /* TEXT */),
            _cache[4] || (_cache[4] = _createElementVNode("button", { class: "quiet" }, "Sign out", -1 /* CACHED */))
          ])
        ]))
      : _createCommentVNode("v-if", true),
    (__props.page.Error)
      ? (_openBlock(), _createElementBlock("p", _hoisted_6, _toDisplayString(__props.page.Error), 1 /* TEXT */))
      : _createCommentVNode("v-if", true),
    (__props.page.View === 'login')
      ? (_openBlock(), _createElementBlock("section", _hoisted_7, [...(_cache[5] || (_cache[5] = [
          _createElementVNode("p", { class: "ledger" }, "Receiver administration", -1 /* CACHED */),
          _createElementVNode("h1", null, "Sign in", -1 /* CACHED */),
          _createElementVNode("form", {
            action: "/admin",
            method: "post",
            class: "stack"
          }, [
            _createElementVNode("label", null, [
              _createTextVNode("Username"),
              _createElementVNode("input", {
                name: "username",
                autocomplete: "username",
                required: "",
                autofocus: ""
              })
            ]),
            _createElementVNode("label", null, [
              _createTextVNode("Password"),
              _createElementVNode("input", {
                type: "password",
                name: "password",
                autocomplete: "current-password",
                required: ""
              })
            ]),
            _createElementVNode("button", { class: "primary" }, "Sign in")
          ], -1 /* CACHED */)
        ]))]))
      : (__props.page.View === 'install')
        ? (_openBlock(), _createElementBlock("section", _hoisted_8, [
            _cache[8] || (_cache[8] = _createElementVNode("h1", null, "Connect your node", -1 /* CACHED */)),
            _cache[9] || (_cache[9] = _createElementVNode("p", null, " Run the installer on the Linux host that runs your orchestrator. It creates a private key on that host and starts the reporter service. ", -1 /* CACHED */)),
            (installCommand.value)
              ? (_openBlock(), _createElementBlock("div", _hoisted_9, [
                  _createElementVNode("label", null, [
                    _cache[6] || (_cache[6] = _createTextVNode("Installation command", -1 /* CACHED */)),
                    _createElementVNode("textarea", {
                      ref_key: "commandField",
                      ref: commandField,
                      readonly: "",
                      rows: "5",
                      spellcheck: "false",
                      value: installCommand.value
                    }, null, 8 /* PROPS */, _hoisted_10)
                  ]),
                  _createElementVNode("button", {
                    class: "primary",
                    onClick: _cache[0] || (_cache[0] = $event => (copy(commandField.value)))
                  }, "Copy command"),
                  _cache[7] || (_cache[7] = _createElementVNode("p", { class: "sub" }, "This command contains your private invitation.", -1 /* CACHED */))
                ]))
              : (_openBlock(), _createElementBlock("p", _hoisted_11, "Open the complete invitation link from your receiver administrator.")),
            _cache[10] || (_cache[10] = _createElementVNode("details", null, [
              _createElementVNode("summary", null, "Manual download and custom socket"),
              _createElementVNode("p", null, [
                _createElementVNode("a", {
                  href: "/install.py",
                  download: "bridge-health-install.py"
                }, "Download the installer"),
                _createTextVNode(" on the node. Run the command below and paste the private installation URL at the prompt. ")
              ]),
              _createElementVNode("pre", null, "sudo python3 -I bridge-health-install.py"),
              _createElementVNode("p", null, [
                _createTextVNode(" The default socket is "),
                _createElementVNode("code", null, "/run/orchestrator-health/health.sock"),
                _createTextVNode(". For a custom path, append "),
                _createElementVNode("code", null, "--socket /absolute/path"),
                _createTextVNode(". ")
              ])
            ], -1 /* CACHED */))
          ]))
        : (__props.page.View === 'overview')
          ? (_openBlock(), _createElementBlock(_Fragment, { key: 4 }, [
              _cache[14] || (_cache[14] = _createElementVNode("div", { class: "heading" }, [
                _createElementVNode("h1", null, "Node overview"),
                _createElementVNode("a", {
                  class: "primary",
                  href: "/invitations#new"
                }, "Invite operator")
              ], -1 /* CACHED */)),
              _createElementVNode("section", _hoisted_12, [
                (_openBlock(true), _createElementBlock(_Fragment, null, _renderList([
          ['Fresh nodes', __props.page.Fresh],
          ['Stale nodes', __props.page.Stale],
          ['Pending invitations', __props.page.Pending],
        ], ([label, count]) => {
                  return (_openBlock(), _createElementBlock("div", { key: label }, [
                    _createElementVNode("span", _hoisted_13, _toDisplayString(label), 1 /* TEXT */),
                    _createElementVNode("strong", null, _toDisplayString(count), 1 /* TEXT */)
                  ]))
                }), 128 /* KEYED_FRAGMENT */))
              ]),
              _createElementVNode("section", _hoisted_14, [
                _cache[13] || (_cache[13] = _createElementVNode("div", { class: "panel-head" }, [
                  _createElementVNode("h2", null, "Reporter nodes"),
                  _createElementVNode("a", { href: "/admin" }, "Refresh")
                ], -1 /* CACHED */)),
                (__props.page.Nodes?.length)
                  ? (_openBlock(), _createElementBlock("div", _hoisted_15, [
                      _createElementVNode("table", null, [
                        _cache[11] || (_cache[11] = _createElementVNode("thead", null, [
                          _createElementVNode("tr", null, [
                            _createElementVNode("th", null, "Node"),
                            _createElementVNode("th", null, "Delivery"),
                            _createElementVNode("th", null, "Socket read"),
                            _createElementVNode("th", null, "Last heartbeat")
                          ])
                        ], -1 /* CACHED */)),
                        _createElementVNode("tbody", null, [
                          (_openBlock(true), _createElementBlock(_Fragment, null, _renderList(__props.page.Nodes, (n) => {
                            return (_openBlock(), _createElementBlock("tr", {
                              key: n.ID
                            }, [
                              _createElementVNode("td", null, [
                                _createElementVNode("a", {
                                  href: '/nodes/' + n.ID
                                }, _toDisplayString(n.Name), 9 /* TEXT, PROPS */, _hoisted_16)
                              ]),
                              _createElementVNode("td", null, [
                                _createElementVNode("span", {
                                  class: "pill",
                                  "data-state": n.Delivery
                                }, _toDisplayString(n.Delivery), 9 /* TEXT, PROPS */, _hoisted_17)
                              ]),
                              _createElementVNode("td", null, _toDisplayString(n.LocalRead || '—'), 1 /* TEXT */),
                              _createElementVNode("td", _hoisted_18, _toDisplayString(stamp(n.LastSeen)), 1 /* TEXT */)
                            ]))
                          }), 128 /* KEYED_FRAGMENT */))
                        ])
                      ])
                    ]))
                  : (_openBlock(), _createElementBlock("div", _hoisted_19, [...(_cache[12] || (_cache[12] = [
                      _createElementVNode("h3", null, "No reporters enrolled", -1 /* CACHED */),
                      _createElementVNode("a", { href: "/invitations#new" }, "Create an invitation", -1 /* CACHED */)
                    ]))]))
              ])
            ], 64 /* STABLE_FRAGMENT */))
          : (__props.page.View === 'tokens')
            ? (_openBlock(), _createElementBlock(_Fragment, { key: 5 }, [
                _cache[20] || (_cache[20] = _createElementVNode("div", { class: "heading" }, [
                  _createElementVNode("h1", null, "API tokens"),
                  _createElementVNode("a", { href: "/api/docs" }, "API guide")
                ], -1 /* CACHED */)),
                (__props.page.IssuedToken)
                  ? (_openBlock(), _createElementBlock("section", _hoisted_20, [
                      _cache[15] || (_cache[15] = _createElementVNode("h2", null, "Copy your token", -1 /* CACHED */)),
                      _cache[16] || (_cache[16] = _createElementVNode("p", null, "This is the only time the full token is shown. Save it in the service’s secret store.", -1 /* CACHED */)),
                      _createElementVNode("div", _hoisted_21, [
                        _createElementVNode("input", {
                          ref_key: "shareField",
                          ref: shareField,
                          "aria-label": "New API token",
                          readonly: "",
                          spellcheck: "false",
                          value: __props.page.IssuedToken
                        }, null, 8 /* PROPS */, _hoisted_22),
                        _createElementVNode("button", {
                          class: "quiet",
                          onClick: _cache[1] || (_cache[1] = $event => (copy(shareField.value)))
                        }, "Copy token")
                      ])
                    ]))
                  : _createCommentVNode("v-if", true),
                _cache[21] || (_cache[21] = _createElementVNode("section", { class: "panel create" }, [
                  _createElementVNode("h2", null, "Issue a read-only token"),
                  _createElementVNode("p", null, [
                    _createTextVNode("Services can read bridge status and heartbeat history. Send the token in the "),
                    _createElementVNode("code", null, "Authorization: Bearer"),
                    _createTextVNode(" header when calling "),
                    _createElementVNode("code", null, "GET /api/v1/status"),
                    _createTextVNode(".")
                  ]),
                  _createElementVNode("form", {
                    class: "inline",
                    method: "post",
                    action: "/admin/tokens"
                  }, [
                    _createElementVNode("label", null, [
                      _createTextVNode("Service name"),
                      _createElementVNode("input", {
                        name: "name",
                        maxlength: "80",
                        required: "",
                        placeholder: "e.g. Operations monitor"
                      })
                    ]),
                    _createElementVNode("label", null, [
                      _createTextVNode("Expires in "),
                      _createElementVNode("select", { name: "days" }, [
                        _createElementVNode("option", { value: "30" }, "30 days"),
                        _createElementVNode("option", {
                          value: "90",
                          selected: ""
                        }, "90 days"),
                        _createElementVNode("option", { value: "365" }, "365 days")
                      ])
                    ]),
                    _createElementVNode("button", { class: "primary" }, "Issue token")
                  ])
                ], -1 /* CACHED */)),
                _createElementVNode("section", _hoisted_23, [
                  _cache[19] || (_cache[19] = _createElementVNode("div", { class: "panel-head" }, [
                    _createElementVNode("h2", null, "Issued tokens")
                  ], -1 /* CACHED */)),
                  (__props.page.Tokens?.length)
                    ? (_openBlock(), _createElementBlock("div", _hoisted_24, [
                        _createElementVNode("table", null, [
                          _cache[18] || (_cache[18] = _createElementVNode("thead", null, [
                            _createElementVNode("tr", null, [
                              _createElementVNode("th", null, "Service"),
                              _createElementVNode("th", null, "Token ID"),
                              _createElementVNode("th", null, "Issued"),
                              _createElementVNode("th", null, "Expires"),
                              _createElementVNode("th", null, "Access")
                            ])
                          ], -1 /* CACHED */)),
                          _createElementVNode("tbody", null, [
                            (_openBlock(true), _createElementBlock(_Fragment, null, _renderList(__props.page.Tokens, (t) => {
                              return (_openBlock(), _createElementBlock("tr", {
                                key: t.ID
                              }, [
                                _createElementVNode("td", null, _toDisplayString(t.Name), 1 /* TEXT */),
                                _createElementVNode("td", _hoisted_25, _toDisplayString(t.ID), 1 /* TEXT */),
                                _createElementVNode("td", null, _toDisplayString(stamp(t.Created)), 1 /* TEXT */),
                                _createElementVNode("td", null, _toDisplayString(stamp(t.Expires)), 1 /* TEXT */),
                                _createElementVNode("td", null, [
                                  _createElementVNode("form", {
                                    method: "post",
                                    action: '/admin/tokens/' + t.ID + '/revoke',
                                    onSubmit: $event => (confirmTokenRevoke($event, t))
                                  }, [...(_cache[17] || (_cache[17] = [
                                    _createElementVNode("button", { class: "quiet danger" }, "Revoke", -1 /* CACHED */)
                                  ]))], 40 /* PROPS, NEED_HYDRATION */, _hoisted_26)
                                ])
                              ]))
                            }), 128 /* KEYED_FRAGMENT */))
                          ])
                        ])
                      ]))
                    : (_openBlock(), _createElementBlock("div", _hoisted_27, "No API tokens issued."))
                ])
              ], 64 /* STABLE_FRAGMENT */))
            : (__props.page.View === 'invites')
              ? (_openBlock(), _createElementBlock(_Fragment, { key: 6 }, [
                  _cache[26] || (_cache[26] = _createElementVNode("div", { class: "heading" }, [
                    _createElementVNode("h1", null, "Invitations")
                  ], -1 /* CACHED */)),
                  (__props.page.Link)
                    ? (_openBlock(), _createElementBlock("section", _hoisted_28, [
                        _cache[22] || (_cache[22] = _createElementVNode("h2", null, "Invitation ready", -1 /* CACHED */)),
                        _cache[23] || (_cache[23] = _createElementVNode("p", null, "Copy this private link before leaving. It expires in 24 hours and enrolls one node.", -1 /* CACHED */)),
                        _createElementVNode("div", _hoisted_29, [
                          _createElementVNode("input", {
                            ref_key: "shareField",
                            ref: shareField,
                            "aria-label": "Private installation URL",
                            readonly: "",
                            value: __props.page.Link
                          }, null, 8 /* PROPS */, _hoisted_30),
                          _createElementVNode("button", {
                            class: "quiet",
                            onClick: _cache[2] || (_cache[2] = $event => (copy(shareField.value)))
                          }, "Copy link")
                        ])
                      ]))
                    : _createCommentVNode("v-if", true),
                  _cache[27] || (_cache[27] = _createElementVNode("section", {
                    id: "new",
                    class: "panel create"
                  }, [
                    _createElementVNode("h2", null, "Invite an operator"),
                    _createElementVNode("p", null, "The pillar name becomes public when the reporter enrolls."),
                    _createElementVNode("form", {
                      class: "inline",
                      method: "post",
                      action: "/invitations"
                    }, [
                      _createElementVNode("label", null, [
                        _createTextVNode("Pillar name"),
                        _createElementVNode("input", {
                          name: "name",
                          maxlength: "80",
                          required: "",
                          placeholder: "e.g. DeeZNNutz.com"
                        })
                      ]),
                      _createElementVNode("button", { class: "primary" }, "Generate link")
                    ])
                  ], -1 /* CACHED */)),
                  _createElementVNode("section", _hoisted_31, [
                    _cache[25] || (_cache[25] = _createElementVNode("div", { class: "panel-head" }, [
                      _createElementVNode("h2", null, "Invitation history")
                    ], -1 /* CACHED */)),
                    (__props.page.Invites?.length)
                      ? (_openBlock(), _createElementBlock("div", _hoisted_32, [
                          _createElementVNode("table", null, [
                            _cache[24] || (_cache[24] = _createElementVNode("thead", null, [
                              _createElementVNode("tr", null, [
                                _createElementVNode("th", null, "Pillar"),
                                _createElementVNode("th", null, "Status"),
                                _createElementVNode("th", null, "Link expires"),
                                _createElementVNode("th", null, "Access")
                              ])
                            ], -1 /* CACHED */)),
                            _createElementVNode("tbody", null, [
                              (_openBlock(true), _createElementBlock(_Fragment, null, _renderList(__props.page.Invites, (n) => {
                                return (_openBlock(), _createElementBlock("tr", {
                                  key: n.ID
                                }, [
                                  _createElementVNode("td", null, [
                                    (n.Enrolled)
                                      ? (_openBlock(), _createElementBlock("a", {
                                          key: 0,
                                          href: '/nodes/' + n.ID
                                        }, _toDisplayString(n.Name), 9 /* TEXT, PROPS */, _hoisted_33))
                                      : (_openBlock(), _createElementBlock(_Fragment, { key: 1 }, [
                                          _createTextVNode(_toDisplayString(n.Name), 1 /* TEXT */)
                                        ], 64 /* STABLE_FRAGMENT */))
                                  ]),
                                  _createElementVNode("td", null, [
                                    _createElementVNode("span", {
                                      class: "pill",
                                      "data-state": n.State
                                    }, _toDisplayString(n.State), 9 /* TEXT, PROPS */, _hoisted_34)
                                  ]),
                                  _createElementVNode("td", _hoisted_35, _toDisplayString(stamp(n.InviteExpires)), 1 /* TEXT */),
                                  _createElementVNode("td", null, [
                                    (['Pending', 'Enrolled'].includes(n.State))
                                      ? (_openBlock(), _createElementBlock("form", {
                                          key: 0,
                                          method: "post",
                                          action: '/nodes/' + n.ID + '/revoke',
                                          onSubmit: $event => (confirmRevoke($event, n))
                                        }, [
                                          _createElementVNode("button", _hoisted_37, _toDisplayString(n.Enrolled ? 'Revoke access' : 'Cancel link'), 1 /* TEXT */)
                                        ], 40 /* PROPS, NEED_HYDRATION */, _hoisted_36))
                                      : (_openBlock(), _createElementBlock("span", _hoisted_38, "—"))
                                  ])
                                ]))
                              }), 128 /* KEYED_FRAGMENT */))
                            ])
                          ])
                        ]))
                      : (_openBlock(), _createElementBlock("div", _hoisted_39, "No invitations yet."))
                  ])
                ], 64 /* STABLE_FRAGMENT */))
              : (_unref(detail))
                ? (_openBlock(), _createElementBlock(_Fragment, { key: 7 }, [
                    _createElementVNode("div", _hoisted_40, [
                      _createElementVNode("h1", null, _toDisplayString(_unref(detail).Name), 1 /* TEXT */),
                      _createElementVNode("span", {
                        class: "pill",
                        "data-state": _unref(detail).Delivery
                      }, _toDisplayString(_unref(detail).Delivery), 9 /* TEXT, PROPS */, _hoisted_41)
                    ]),
                    _createElementVNode("section", _hoisted_42, [
                      _cache[34] || (_cache[34] = _createElementVNode("h2", null, "Connection", -1 /* CACHED */)),
                      _createElementVNode("dl", null, [
                        _cache[28] || (_cache[28] = _createElementVNode("dt", null, "Stream ID", -1 /* CACHED */)),
                        _createElementVNode("dd", null, _toDisplayString(_unref(detail).ID), 1 /* TEXT */),
                        _cache[29] || (_cache[29] = _createElementVNode("dt", null, "Certificate expires", -1 /* CACHED */)),
                        _createElementVNode("dd", null, _toDisplayString(stamp(_unref(detail).CertExpires)), 1 /* TEXT */),
                        _cache[30] || (_cache[30] = _createElementVNode("dt", null, "Last heartbeat", -1 /* CACHED */)),
                        _createElementVNode("dd", null, _toDisplayString(stamp(_unref(detail).LastSeen)), 1 /* TEXT */),
                        _cache[31] || (_cache[31] = _createElementVNode("dt", null, "Sequence", -1 /* CACHED */)),
                        _createElementVNode("dd", null, _toDisplayString(_unref(detail).Sequence), 1 /* TEXT */),
                        _cache[32] || (_cache[32] = _createElementVNode("dt", null, "Socket read", -1 /* CACHED */)),
                        _createElementVNode("dd", null, _toDisplayString(_unref(detail).LocalRead || '—'), 1 /* TEXT */)
                      ]),
                      (_unref(detail).State === 'Enrolled')
                        ? (_openBlock(), _createElementBlock("form", {
                            key: 0,
                            method: "post",
                            action: '/nodes/' + _unref(detail).ID + '/revoke',
                            onSubmit: _cache[3] || (_cache[3] = $event => (confirmRevoke($event, _unref(detail))))
                          }, [...(_cache[33] || (_cache[33] = [
                            _createElementVNode("button", { class: "quiet danger" }, "Revoke access", -1 /* CACHED */)
                          ]))], 40 /* PROPS, NEED_HYDRATION */, _hoisted_43))
                        : _createCommentVNode("v-if", true)
                    ]),
                    (_unref(detail).State === 'Enrolled')
                      ? (_openBlock(), _createElementBlock("section", _hoisted_44, [
                          _cache[37] || (_cache[37] = _createElementVNode("h2", null, "Public listing", -1 /* CACHED */)),
                          _cache[38] || (_cache[38] = _createElementVNode("p", null, " Known pillar names use the explorer’s producer address. Save a replacement or clear the field to hide the address. ", -1 /* CACHED */)),
                          _createElementVNode("form", {
                            method: "post",
                            action: '/nodes/' + _unref(detail).ID + '/producer',
                            class: "inline"
                          }, [
                            _createElementVNode("label", null, [
                              _cache[35] || (_cache[35] = _createTextVNode("Producer address (optional)", -1 /* CACHED */)),
                              _createElementVNode("input", {
                                name: "producer",
                                value: _unref(detail).Producer,
                                maxlength: "40",
                                pattern: "z1[023456789acdefghjklmnpqrstuvwxyz]{38}",
                                placeholder: "z1…",
                                spellcheck: "false"
                              }, null, 8 /* PROPS */, _hoisted_46)
                            ]),
                            _cache[36] || (_cache[36] = _createElementVNode("button", { class: "primary" }, "Save address", -1 /* CACHED */))
                          ], 8 /* PROPS */, _hoisted_45)
                        ]))
                      : _createCommentVNode("v-if", true),
                    (_unref(detail).Snapshot)
                      ? (_openBlock(), _createElementBlock("details", _hoisted_47, [
                          _cache[39] || (_cache[39] = _createElementVNode("summary", null, "Latest heartbeat and last successful snapshot", -1 /* CACHED */)),
                          _createElementVNode("pre", null, _toDisplayString(_unref(detail).Snapshot), 1 /* TEXT */)
                        ]))
                      : _createCommentVNode("v-if", true)
                  ], 64 /* STABLE_FRAGMENT */))
                : _createCommentVNode("v-if", true),
    _createElementVNode("p", _hoisted_48, _toDisplayString(copied.value), 1 /* TEXT */)
  ], 64 /* STABLE_FRAGMENT */))
}
}

}