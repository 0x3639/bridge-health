<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'

const data = ref(null)
const failure = ref(false)
const loading = ref(false)
const query = ref('')
const filter = ref('All statuses')
const chains = ['BNB', 'ETH']
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
</script>

<template>
  <div class="heading">
    <div>
      <h1>Pillar status</h1>
      <p class="sub">Health reports from enrolled bridge orchestrators.</p>
    </div>
    <button class="quiet refresh" :disabled="loading" @click="refresh">
      <span aria-hidden="true">↻</span> {{ loading ? 'Refreshing…' : 'Refresh' }}
    </button>
  </div>
  <p v-if="failure" class="error" role="alert">
    Status refresh failed.
    {{ data ? 'Showing the last received update.' : 'Try refreshing again.' }}
  </p>
  <section class="panel">
    <div class="panel-head">
      <span class="last-update" aria-live="polite"
        ><template v-if="data"
          >Last update
          <time :datetime="data.updated">{{ formatTime(data.updated) }} UTC</time></template
        ><template v-else>Waiting for status</template></span
      >
      <div class="tools">
        <input
          v-model="query"
          type="search"
          aria-label="Find a pillar or producer"
          placeholder="Find a pillar or producer"
        /><select v-model="filter" aria-label="Filter by status">
          <option>All statuses</option>
          <option v-for="status in statuses" :key="status">{{ status }}</option>
        </select>
      </div>
    </div>
    <div class="scroll" role="region" aria-label="Pillar status table" tabindex="0">
      <table class="status-table">
        <thead>
          <tr>
            <th scope="col">Pillar</th>
            <th scope="col">Status</th>
            <th scope="col">State</th>
            <th scope="col">Producer</th>
            <th
              v-for="chain in chains"
              :key="chain"
              scope="col"
              title="Observed unsigned wraps / unwraps"
            >
              {{ chain }}
            </th>
            <th scope="col">Resp. time</th>
            <th scope="col">Last checked <small>UTC</small></th>
            <th scope="col">Error</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="node in filtered" :key="node.id">
            <th scope="row" class="pillar">
              <a
                v-if="node.producer"
                :href="'https://explorer.zenon.info/pillar/' + node.producer"
                >{{ node.name }}</a
              ><template v-else>{{ node.name }}</template>
            </th>
            <td data-label="Status">
              <span class="pill" :data-state="node.status">{{ node.status }}</span>
            </td>
            <td data-label="State" :title="stateNames[node.phase]">
              {{ stateNames[node.state] || stateNames[node.phase] || '—' }}
            </td>
            <td data-label="Producer">
              <a
                v-if="node.producer"
                class="mono"
                :title="node.producer"
                :href="'https://explorer.zenon.info/address/' + node.producer"
                >{{ node.producer.slice(0, 5) }}…{{ node.producer.slice(-5) }}</a
              ><span v-else>—</span>
            </td>
            <td
              v-for="chain in chains"
              :key="chain"
              :data-label="chain"
              class="mono"
              :title="queueTitle(node.queues?.[chain])"
            >
              <template v-if="node.queues?.[chain]"
                >{{ number(node.queues[chain].wrap) }} /
                {{ number(node.queues[chain].unwrap) }}</template
              ><template v-else>—</template>
            </td>
            <td data-label="Resp. time" class="mono" title="Reporter’s local snapshot read time">
              {{
                node.readMicros == null
                  ? '—'
                  : (node.readMicros / 1000).toLocaleString(undefined, {
                      maximumFractionDigits: 2,
                    }) + ' ms'
              }}
            </td>
            <td data-label="Last checked (UTC)" class="mono">
              <time v-if="node.checked" :datetime="node.checked">{{
                formatTime(node.checked)
              }}</time
              ><template v-else>—</template>
            </td>
            <td data-label="Error" :class="{ issue: node.error }">{{ node.error || '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="!filtered.length" class="empty" role="status">
      <h3>
        {{ emptyMessage }}
      </h3>
      <p v-if="data && !nodes.length">Pillars appear here when their reporter enrolls.</p>
      <button
        v-if="data && nodes.length"
        class="quiet"
        @click="clearFilters"
      >
        Clear filters
      </button>
    </div>
  </section>
  <details class="guide">
    <summary>How to read this table</summary>
    <p>
      <b>Online</b> means a recent heartbeat contained a valid local snapshot.
      <b>Unavailable</b> means the reporter checked in with a socket error. A heartbeat becomes
      <b>stale</b> after three minutes.
    </p>
    <p>
      Network columns show observed unsigned wraps / unwraps. An asterisk marks a partial count. A
      dash means the measurement is unavailable. Hover over a pair for its observation times.
    </p>
    <p>
      Response time measures a successful snapshot read on the operator’s host. Last checked is the
      receiver’s heartbeat receipt time in UTC. This page refreshes every 30 seconds.
    </p>
  </details>
  <p class="guide"><a href="/api/docs">Status API guide</a></p>
</template>
