<script setup>
import { computed, ref } from 'vue'

const { page } = defineProps({ page: Object })
const detail = page.Detail
const copied = ref('')
const shareField = ref(null)
const commandField = ref(null)
const token = location.hash.slice(1)
const installCommand = computed(() => {
  if (page.View !== 'install' || !/^[a-f0-9-]{36}\.[A-Za-z0-9_-]{43}$/.test(token)) return ''
  return `installer=$(curl -fsS '${page.Origin}/install.py') && printf '%s\\n' '${token}' | sudo python3 -I -c "$installer"`
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
</script>
<template>
  <nav v-if="page.User" class="admin-nav" aria-label="Administration">
    <a href="/admin" :aria-current="['overview', 'detail'].includes(page.View) ? 'page' : null">Overview</a>
    <a href="/invitations" :aria-current="page.View === 'invites' ? 'page' : null">Invitations</a>
    <a href="/admin/tokens" :aria-current="page.View === 'tokens' ? 'page' : null">API tokens</a>
    <form method="post" action="/logout">
      <span>{{ page.User }}</span
      ><button class="quiet">Sign out</button>
    </form>
  </nav>
  <p v-if="page.Error" role="alert" class="error">{{ page.Error }}</p>
  <section v-if="page.View === 'login'" class="panel access">
    <p class="ledger">Receiver administration</p>
    <h1>Sign in</h1>
    <form action="/admin" method="post" class="stack">
      <label>Username<input name="username" autocomplete="username" required autofocus /></label>
      <label
        >Password<input type="password" name="password" autocomplete="current-password" required
      /></label>
      <button class="primary">Sign in</button>
    </form>
  </section>
  <section v-else-if="page.View === 'install'" class="panel access">
    <h1>Connect your node</h1>
    <p>
      Run the installer on the Linux host that runs your orchestrator. It creates a private key on
      that host and starts the reporter service.
    </p>
    <div v-if="installCommand" class="stack">
      <label
        >Installation command<textarea
          ref="commandField"
          readonly
          rows="5"
          spellcheck="false"
          :value="installCommand"
        ></textarea>
      </label>
      <button class="primary" @click="copy(commandField)">Copy command</button>
      <p class="sub">This command contains your private invitation.</p>
    </div>
    <p v-else class="error">Open the complete invitation link from your receiver administrator.</p>
    <details>
      <summary>Manual download and custom socket</summary>
      <p>
        <a href="/install.py" download="bridge-health-install.py">Download the installer</a> on the
        node. Run the command below and paste the private installation URL at the prompt.
      </p>
      <pre>sudo python3 -I bridge-health-install.py</pre>
      <p>
        The default socket is <code>/run/orchestrator-health/health.sock</code>. For a custom path,
        append <code>--socket /absolute/path</code>.
      </p>
    </details>
  </section>
  <template v-else-if="page.View === 'overview'">
    <div class="heading">
      <h1>Node overview</h1>
      <a class="primary" href="/invitations#new">Invite operator</a>
    </div>
    <section class="metrics" aria-label="Fleet summary">
      <div
        v-for="[label, count] in [
          ['Fresh nodes', page.Fresh],
          ['Stale nodes', page.Stale],
          ['Pending invitations', page.Pending],
        ]"
        :key="label"
      >
        <span class="metric-label">{{ label }}</span
        ><strong>{{ count }}</strong>
      </div>
    </section>
    <section class="panel">
      <div class="panel-head">
        <h2>Reporter nodes</h2>
        <a href="/admin">Refresh</a>
      </div>
      <div v-if="page.Nodes?.length" class="scroll">
        <table>
          <thead>
            <tr>
              <th>Node</th>
              <th>Delivery</th>
              <th>Socket read</th>
              <th>Last heartbeat</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in page.Nodes" :key="n.ID">
              <td>
                <a :href="'/nodes/' + n.ID">{{ n.Name }}</a>
              </td>
              <td>
                <span class="pill" :data-state="n.Delivery">{{ n.Delivery }}</span>
              </td>
              <td>{{ n.LocalRead || '—' }}</td>
              <td class="mono">{{ stamp(n.LastSeen) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <h3>No reporters enrolled</h3>
        <a href="/invitations#new">Create an invitation</a>
      </div>
    </section>
  </template>
  <template v-else-if="page.View === 'tokens'">
    <div class="heading"><h1>API tokens</h1><a href="/api/docs">API guide</a></div>
    <section v-if="page.IssuedToken" class="panel create notice">
      <h2>Copy your token</h2>
      <p>This is the only time the full token is shown. Save it in the service’s secret store.</p>
      <div class="inline">
        <input ref="shareField" aria-label="New API token" readonly spellcheck="false" :value="page.IssuedToken" />
        <button class="quiet" @click="copy(shareField)">Copy token</button>
      </div>
    </section>
    <section class="panel create">
      <h2>Issue a read-only token</h2>
      <p>Services can read bridge status and heartbeat history. Send the token in the
        <code>Authorization: Bearer</code> header when calling <code>GET /api/v1/status</code>.</p>
      <form class="inline" method="post" action="/admin/tokens">
        <label>Service name<input name="name" maxlength="80" required placeholder="e.g. Operations monitor" /></label>
        <label>Expires in
          <select name="days">
            <option value="30">30 days</option>
            <option value="90" selected>90 days</option>
            <option value="365">365 days</option>
          </select>
        </label>
        <button class="primary">Issue token</button>
      </form>
    </section>
    <section class="panel">
      <div class="panel-head"><h2>Issued tokens</h2></div>
      <div v-if="page.Tokens?.length" class="scroll">
        <table>
          <thead><tr><th>Service</th><th>Token ID</th><th>Issued</th><th>Expires</th><th>Access</th></tr></thead>
          <tbody>
            <tr v-for="t in page.Tokens" :key="t.ID">
              <td>{{ t.Name }}</td>
              <td class="mono">{{ t.ID }}</td>
              <td>{{ stamp(t.Created) }}</td>
              <td>{{ stamp(t.Expires) }}</td>
              <td>
                <form method="post" :action="'/admin/tokens/' + t.ID + '/revoke'"
                  @submit="confirmTokenRevoke($event, t)">
                  <button class="quiet danger">Revoke</button>
                </form>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">No API tokens issued.</div>
    </section>
  </template>
  <template v-else-if="page.View === 'invites'">
    <div class="heading"><h1>Invitations</h1></div>
    <section v-if="page.Link" class="panel create notice">
      <h2>Invitation ready</h2>
      <p>Copy this private link before leaving. It expires in 24 hours and enrolls one node.</p>
      <div class="inline">
        <input
          ref="shareField"
          aria-label="Private installation URL"
          readonly
          :value="page.Link"
        /><button class="quiet" @click="copy(shareField)">Copy link</button>
      </div>
    </section>
    <section id="new" class="panel create">
      <h2>Invite an operator</h2>
      <p>The pillar name becomes public when the reporter enrolls.</p>
      <form class="inline" method="post" action="/invitations">
        <label
          >Pillar name<input
            name="name"
            maxlength="80"
            required
            placeholder="e.g. DeeZNNutz.com" /></label
        ><button class="primary">Generate link</button>
      </form>
    </section>
    <section class="panel">
      <div class="panel-head"><h2>Invitation history</h2></div>
      <div v-if="page.Invites?.length" class="scroll">
        <table>
          <thead>
            <tr>
              <th>Pillar</th>
              <th>Status</th>
              <th>Link expires</th>
              <th>Access</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in page.Invites" :key="n.ID">
              <td>
                <a v-if="n.Enrolled" :href="'/nodes/' + n.ID">{{ n.Name }}</a
                ><template v-else>{{ n.Name }}</template>
              </td>
              <td>
                <span class="pill" :data-state="n.State">{{ n.State }}</span>
              </td>
              <td class="mono">{{ stamp(n.InviteExpires) }}</td>
              <td>
                <form
                  v-if="['Pending', 'Enrolled'].includes(n.State)"
                  method="post"
                  :action="'/nodes/' + n.ID + '/revoke'"
                  @submit="confirmRevoke($event, n)"
                >
                  <button class="quiet">{{ n.Enrolled ? 'Revoke access' : 'Cancel link' }}</button>
                </form>
                <span v-else>—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">No invitations yet.</div>
    </section>
  </template>
  <template v-else-if="detail">
    <div class="heading">
      <h1>{{ detail.Name }}</h1>
      <span class="pill" :data-state="detail.Delivery">{{ detail.Delivery }}</span>
    </div>
    <section class="panel create">
      <h2>Connection</h2>
      <dl>
        <dt>Stream ID</dt>
        <dd>{{ detail.ID }}</dd>
        <dt>Certificate expires</dt>
        <dd>{{ stamp(detail.CertExpires) }}</dd>
        <dt>Last heartbeat</dt>
        <dd>{{ stamp(detail.LastSeen) }}</dd>
        <dt>Sequence</dt>
        <dd>{{ detail.Sequence }}</dd>
        <dt>Socket read</dt>
        <dd>{{ detail.LocalRead || '—' }}</dd>
      </dl>
      <form
        v-if="detail.State === 'Enrolled'"
        method="post"
        :action="'/nodes/' + detail.ID + '/revoke'"
        @submit="confirmRevoke($event, detail)"
      >
        <button class="quiet danger">Revoke access</button>
      </form>
    </section>
    <section v-if="detail.State === 'Enrolled'" class="panel create">
      <h2>Public listing</h2>
      <p>
        Known pillar names use the explorer’s producer address. Save a replacement or clear the
        field to hide the address.
      </p>
      <form method="post" :action="'/nodes/' + detail.ID + '/producer'" class="inline">
        <label
          >Producer address (optional)<input
            name="producer"
            :value="detail.Producer"
            maxlength="40"
            pattern="z1[023456789acdefghjklmnpqrstuvwxyz]{38}"
            placeholder="z1…"
            spellcheck="false"
        /></label>
        <button class="primary">Save address</button>
      </form>
    </section>
    <details v-if="detail.Snapshot" class="panel create">
      <summary>Latest heartbeat and last successful snapshot</summary>
      <pre>{{ detail.Snapshot }}</pre>
    </details>
  </template>
  <p role="status" class="copy-status">{{ copied }}</p>
</template>
