<template>
  <v-container fluid class="fleet-shell">
    <div class="fleet-shell__glow fleet-shell__glow--one"></div>
    <div class="fleet-shell__glow fleet-shell__glow--two"></div>

    <div class="fleet-shell__inner">
      <v-card class="fleet-hero" rounded="xl" variant="flat">
        <div class="fleet-hero__topline">
          <span class="fleet-hero__badge">{{ $t('ui.fleet.badge') }}</span>
          <span class="fleet-hero__badge fleet-hero__badge--soft">
            {{ initialLoading ? $t('ui.fleet.checking') : `${reachableCount}/${servers.length} ${$t('ui.common.online')}` }}
          </span>
        </div>
        <v-row class="fleet-hero__content" align="center">
          <v-col cols="12" lg="7">
            <div class="fleet-hero__title-row">
              <div class="fleet-hero__icon">
                <v-icon icon="mdi-server-network" size="32" />
              </div>
              <div>
                <h1 class="fleet-hero__title">{{ $t('ui.fleet.title') }}</h1>
                <p class="fleet-hero__subtitle">{{ $t('ui.fleet.subtitle') }}</p>
              </div>
            </div>
            <div class="fleet-hero__meta">
              <span>{{ $t('ui.fleet.lastCheck', { time: formattedCheckedAt }) }}</span>
              <span>•</span>
              <span>{{ $t('ui.fleet.remoteCount', { count: remoteCount }) }}</span>
              <span>•</span>
              <span>{{ $t('ui.fleet.autoRefresh') }}</span>
            </div>
          </v-col>
          <v-col cols="12" lg="5" class="fleet-hero__actions">
            <v-btn
              variant="outlined"
              color="warning"
              :loading="batchAction === 'restart'"
              :disabled="loading || batchAction !== ''"
              @click="runBatchAction('restart')"
            >
              <v-icon icon="mdi-restart" start />
              {{ $t('ui.fleet.batchRestart') }}
            </v-btn>
            <v-btn
              color="primary"
              :loading="batchAction === 'update'"
              :disabled="loading || batchAction !== ''"
              @click="runBatchAction('update')"
            >
              <v-icon icon="mdi-download-outline" start />
              {{ $t('ui.fleet.batchUpdate') }}
            </v-btn>
            <v-btn variant="outlined" :disabled="loading" @click="showConfig = true">
              <v-icon icon="mdi-server-plus" start />
              {{ $t('ui.fleet.manage') }}
            </v-btn>
            <v-btn variant="tonal" color="primary" :disabled="loading" @click="openRemoteInboundDialog">
              <v-icon icon="mdi-server-plus" start />
              {{ $t('ui.fleet.remoteInboundCreate') }}
            </v-btn>
            <v-btn color="primary" :loading="loading" @click="loadFleet">
              <v-icon icon="mdi-refresh" start />
              {{ $t('ui.fleet.refreshStatus') }}
            </v-btn>
          </v-col>
        </v-row>
        <v-alert v-if="batchMessage" class="fleet-batch-alert" variant="tonal" :type="batchMessageType">
          {{ batchMessage }}
        </v-alert>
      </v-card>

      <v-row class="fleet-summary" density="comfortable">
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--one" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.total') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : servers.length }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--two" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.common.online') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : reachableCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--three" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.coreRunning') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : runningCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--four" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.errors') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : errorCount }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--five" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.onlineUsers') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : onlineUsersTotal }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col">
          <v-card class="fleet-summary__card fleet-summary__card--six" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.endpointTotal') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : endpointTotal }}</div>
          </v-card>
        </v-col>
        <v-col cols="6" class="fleet-summary__col fleet-summary__col--last">
          <v-card class="fleet-summary__card fleet-summary__card--seven" rounded="xl" variant="flat">
            <div class="fleet-summary__label">{{ $t('ui.fleet.configDrift') }}</div>
            <div class="fleet-summary__value">{{ initialLoading ? '—' : driftServerCount }}</div>
          </v-card>
        </v-col>
      </v-row>

      <v-alert v-if="!initialLoading && servers.length === 1" type="info" variant="tonal" rounded="xl" class="fleet-empty">
        {{ $t('ui.fleet.onlyLocal') }}
      </v-alert>

      <v-row class="fleet-grid" density="comfortable">
        <v-col v-for="server in servers" :key="server.id" cols="12" md="6" xl="4">
          <v-card class="fleet-card" rounded="xl" variant="flat" @click="openDetails(server)">
            <div class="fleet-card__header">
              <div class="fleet-card__identity">
                <div class="fleet-card__icon" :class="statusClass(server)">
                  <v-icon :icon="server.id === 'local' ? 'mdi-home' : 'mdi-server'" />
                </div>
                <div class="fleet-card__name-wrap">
                  <div class="fleet-card__name">{{ server.name }}</div>
                  <div class="fleet-card__url">{{ server.url }}</div>
                </div>
              </div>
              <div class="fleet-card__chips">
                <v-chip v-if="server.driftCount" size="small" color="warning" variant="tonal">{{ $t('ui.fleet.driftItems', { count: server.driftCount }) }}</v-chip>
                <v-chip size="small" :color="server.reachable ? 'success' : server.enabled ? 'error' : 'secondary'" variant="flat">
                  {{ server.reachable ? $t('ui.common.online') : server.lastKnown ? $t('ui.fleet.lastKnown') : server.enabled ? $t('ui.fleet.unreachable') : $t('ui.fleet.disabled') }}
                </v-chip>
              </div>
            </div>

            <v-divider />

            <div class="fleet-monitor">
              <div class="fleet-monitor__item fleet-monitor__item--cpu">
                <div class="fleet-monitor__head">
                  <span>{{ $t('ui.common.cpu') }}</span>
                  <strong>{{ server.reachable && server.ResourcesReady ? formatPercent(server.CPUPercent) : '-' }}</strong>
                </div>
                <v-progress-linear :model-value="server.reachable && server.ResourcesReady ? clampPercent(server.CPUPercent) : 0" height="5" rounded color="info" />
              </div>
              <div class="fleet-monitor__item fleet-monitor__item--memory">
                <div class="fleet-monitor__head">
                  <span>{{ $t('ui.common.memory') }}</span>
                  <strong>{{ server.reachable && server.ResourcesReady ? formatMemory(server.MemoryUsed, server.MemoryTotal) : '-' }}</strong>
                </div>
                <v-progress-linear :model-value="server.reachable && server.ResourcesReady ? memoryPercent(server) : 0" height="5" rounded color="warning" />
              </div>
              <div class="fleet-monitor__item fleet-monitor__item--upload">
                <span>{{ $t('ui.common.uploadSpeed') }}</span>
                <strong>{{ networkRateLabel(server, 'upload') }}</strong>
              </div>
              <div class="fleet-monitor__item fleet-monitor__item--download">
                <span>{{ $t('ui.common.downloadSpeed') }}</span>
                <strong>{{ networkRateLabel(server, 'download') }}</strong>
              </div>
            </div>

            <div class="fleet-traffic">
              <div class="fleet-traffic__head">
                <div class="fleet-traffic__title">
                  <span>{{ trafficTitle(server) }}</span>
                  <v-chip
                    v-if="trafficBudgetReadable(server)"
                    :color="trafficBudgetColor(server)"
                    size="x-small"
                    variant="tonal"
                  >
                    {{ trafficBudgetStatusLabel(server) }}
                  </v-chip>
                </div>
                <strong>{{ trafficTotalLabel(server) }}</strong>
              </div>
              <v-progress-linear
                v-if="trafficBudgetReadable(server) && (server.reachable || server.lastKnown)"
                :model-value="trafficBudgetPercent(server)"
                :color="trafficBudgetColor(server)"
                height="6"
                rounded
              />
              <small>{{ trafficDetailLabel(server) }}</small>
            </div>

            <div class="fleet-card__metrics">
              <div class="fleet-metric">
                <span>{{ $t('ui.common.version') }}</span>
                <strong>{{ server.System?.appVersion || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.common.latency') }}</span>
                <strong>{{ server.id === 'local' ? $t('ui.common.local') : server.reachable ? `${server.latencyMs} ms` : '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.common.publicIp') }}</span>
                <strong>{{ server.PublicIP || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.common.uptime') }}</span>
                <strong>{{ formatUptime(server.Uptime) }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.common.firewall') }}</span>
                <strong>{{ server.portBackend || '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.fleet.listenersNat') }}</span>
                <strong>{{ server.reachable ? `${server.listeners} / ${server.natRules}` : '-' }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.fleet.usersOnline') }}</span>
                <strong>{{ server.OnlineUsers }} / {{ server.Clients }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.fleet.inboundsOutbounds') }}</span>
                <strong>{{ server.Inbounds }} / {{ server.Outbounds }}</strong>
              </div>
              <div class="fleet-metric">
                <span>{{ $t('ui.common.endpoints') }}</span>
                <strong>{{ server.Endpoints }}</strong>
              </div>
              <div class="fleet-metric">
                <span>MASQUE</span>
                <strong>{{ server.MasqueRunning }} / {{ server.MasqueTotal }}</strong>
              </div>
              <div class="fleet-metric">
                <span>Mieru</span>
                <strong>{{ server.MieruRunning }} / {{ server.MieruTotal }}</strong>
              </div>
            </div>

            <div class="fleet-card__footer">
              <span class="fleet-core-state" :class="server.Core?.running ? 'is-running' : ''">
                <v-icon :icon="server.Core?.running ? 'mdi-check-circle' : 'mdi-alert-circle-outline'" size="16" />
                {{ server.Core?.running ? $t('ui.fleet.singboxRunning') : $t('ui.fleet.singboxStopped') }}
              </span>
              <span v-if="server.error" class="fleet-card__error" :title="server.error">{{ server.error }}</span>
            </div>
            <div class="fleet-card__actions">
              <v-btn size="small" variant="tonal" @click.stop="openDetails(server)">
                <v-icon icon="mdi-information-outline" start />{{ $t('ui.common.details') }}
              </v-btn>
              <v-btn size="small" variant="text" @click.stop="openLogs(server)">
                <v-icon icon="mdi-text-box-outline" start />{{ $t('ui.common.logs') }}
              </v-btn>
              <v-btn size="small" variant="text" color="warning" :disabled="batchAction !== '' || !server.reachable" @click.stop="restartServer(server)">
                <v-icon icon="mdi-restart" start />{{ $t('ui.common.restart') }}
              </v-btn>
              <v-btn size="small" variant="text" color="primary" :loading="updateLoadingId === server.id" :disabled="batchAction !== '' || !server.reachable" @click.stop="updateServer(server)">
                <v-icon icon="mdi-download-outline" start />{{ $t('ui.common.update') }}
              </v-btn>
              <v-btn size="small" variant="text" :loading="refreshLoadingId === server.id" @click.stop="refreshServer(server)">
                <v-icon icon="mdi-refresh" start />{{ $t('ui.common.refresh') }}
              </v-btn>
            </div>
          </v-card>
        </v-col>
      </v-row>
    </div>

    <v-dialog v-model="showConfig" max-width="860" scrollable>
      <v-card rounded="xl" class="fleet-dialog">
        <v-card-title class="fleet-dialog__title">
          <span>{{ $t('ui.fleet.manageTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="showConfig = false" />
        </v-card-title>
        <v-card-subtitle>
          {{ $t('ui.fleet.manageHint') }}
        </v-card-subtitle>
        <v-card-text>
          <div v-for="(item, index) in configs" :key="item.id || index" class="fleet-config-row">
            <v-text-field v-model="item.name" :label="$t('ui.fleet.name')" density="compact" hide-details />
            <v-text-field v-model="item.url" :label="$t('ui.fleet.panelUrl')" density="compact" hide-details />
            <v-text-field
              v-model="item.token"
              :label="$t('ui.fleet.apiToken')"
              density="compact"
              hide-details
              type="password"
              :placeholder="item.tokenSet ? $t('ui.fleet.tokenSaved') : ''"
            />
            <v-switch v-model="item.enabled" color="primary" hide-details density="compact" />
            <v-btn icon="mdi-delete-outline" color="error" variant="text" :aria-label="$t('actions.del')" @click="removeConfig(index)" />
          </div>
          <v-btn variant="tonal" color="primary" class="fleet-dialog__add" @click="addConfig">
            <v-icon icon="mdi-plus" start />
            {{ $t('ui.fleet.addServer') }}
          </v-btn>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showConfig = false">{{ $t('ui.common.cancel') }}</v-btn>
          <v-btn color="primary" :loading="saving" @click="saveConfig">{{ $t('ui.fleet.saveCheck') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="showRemoteInbound" max-width="980" scrollable>
      <v-card rounded="xl" class="fleet-dialog fleet-remote-inbound">
        <header class="fleet-remote-inbound__header">
          <div>
            <span class="fleet-remote-inbound__eyebrow">{{ $t('ui.fleet.remoteInboundEyebrow') }}</span>
            <h2>{{ $t('ui.fleet.remoteInboundTitle') }}</h2>
            <p>{{ $t('ui.fleet.remoteInboundHint') }}</p>
          </div>
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="showRemoteInbound = false" />
        </header>
        <v-card-text class="fleet-remote-inbound__body">
          <v-alert type="info" variant="tonal" density="compact">{{ $t('ui.fleet.remoteInboundSafety') }}</v-alert>
          <FleetInboundDraftEditor v-model="remoteInboundDraft" />

          <section class="fleet-remote-inbound__section">
            <div class="fleet-remote-inbound__section-head">
              <div class="fleet-remote-inbound__section-icon"><v-icon icon="mdi-server-network-outline" /></div>
              <div><h3>{{ $t('ui.fleet.targetServers') }}</h3><p>{{ $t('ui.fleet.remoteInboundTargetsHint') }}</p></div>
            </div>
            <div class="fleet-target-grid">
              <button
                v-for="server in remoteInboundServers"
                :key="server.id"
                type="button"
                :aria-pressed="remoteInboundTargetIds.includes(server.id)"
                :class="{ 'is-selected': remoteInboundTargetIds.includes(server.id) }"
                @click="toggleRemoteInboundTarget(server.id)"
              >
                <span class="fleet-target-grid__check"><v-icon :icon="remoteInboundTargetIds.includes(server.id) ? 'mdi-check' : 'mdi-plus'" /></span>
                <span><strong>{{ server.name }}</strong><small>{{ server.url }}</small></span>
                <em>{{ server.latencyMs ? `${server.latencyMs} ms` : $t('ui.common.online') }}</em>
              </button>
            </div>
            <v-alert v-if="!remoteInboundServers.length" type="warning" variant="tonal" density="compact">{{ $t('ui.fleet.remoteInboundNoServers') }}</v-alert>
          </section>

          <div v-if="remoteInboundTargetIds.length" class="fleet-remote-inbound__targets">
            <v-progress-linear v-if="remoteInboundLoading === 'targets'" indeterminate color="primary" />
            <article v-for="id in remoteInboundTargetIds" :key="id" class="fleet-remote-target">
              <header>
                <div><strong>{{ remoteInboundServer(id)?.name || id }}</strong><small>{{ remoteTargetInfo(id)?.capabilities?.subscriptionHost || remoteTargetInfo(id)?.error || $t('ui.fleet.remoteInboundWaiting') }}</small></div>
                <v-chip v-if="remoteTargetInfo(id)?.success" size="small" color="success" variant="tonal">{{ $t('ui.common.online') }}</v-chip>
                <v-chip v-else-if="remoteTargetInfo(id)?.error" size="small" color="error" variant="tonal">{{ $t('ui.fleet.failed') }}</v-chip>
              </header>
              <div v-if="remoteTargetInfo(id)?.success && remoteTargetSettings[id]" class="fleet-remote-target__settings">
                <v-text-field
                  :model-value="remoteTargetSettings[id].tag"
                  :label="$t('ui.fleet.remoteInboundGeneratedName')"
                  :hint="$t('ui.fleet.remoteInboundNameHint')"
                  :error-messages="remoteInboundTagError(id)"
                  :disabled="remoteInboundLoading !== ''"
                  variant="outlined"
                  density="comfortable"
                  hide-details="auto"
                  @update:model-value="updateRemoteInboundTag(id, $event)"
                />
                <v-text-field
                  v-model.number="remoteTargetSettings[id].listenPort"
                  type="number"
                  min="1"
                  max="65535"
                  :label="$t('ui.fleet.remoteInboundPort')"
                  variant="outlined"
                  density="comfortable"
                  hide-details
                  @update:model-value="clearRemoteInboundPreview"
                />
                <v-select
                  v-if="remoteInboundTLSCapable"
                  v-model="remoteTargetSettings[id].tlsId"
                  :items="remoteTlsItems(id)"
                  :label="$t('ui.fleet.remoteInboundTLS')"
                  :placeholder="remoteInboundTLSRequired ? $t('ui.fleet.remoteInboundTLSSelect') : $t('ui.fleet.remoteInboundTLSOptional')"
                  :clearable="!remoteInboundTLSRequired"
                  item-title="title"
                  item-value="value"
                  variant="outlined"
                  density="comfortable"
                  hide-details
                  @update:model-value="clearRemoteInboundPreview"
                />
                <v-alert v-if="remoteInboundTLSRequired && remoteTlsItems(id).length === 0" class="fleet-remote-target__certificate" type="warning" variant="tonal" density="compact">
                  {{ $t('ui.fleet.remoteInboundTLSUnavailable') }}
                </v-alert>
                <div v-if="remoteInboundTLSCapable && remoteTargetSettings[id]?.tlsId" class="fleet-remote-target__certificate">
                  <span>{{ $t('ui.fleet.remoteInboundCertificate') }}: {{ remoteTargetTlsSummary(id) }}</span>
                </div>
                <v-alert
                  v-if="remoteInboundDraft.type === 'masque'"
                  type="info"
                  variant="tonal"
                  density="compact"
                >{{ $t('ui.fleet.remoteInboundMasqueTLS') }}</v-alert>
              </div>
              <p v-else-if="remoteTargetInfo(id)?.error" class="fleet-remote-target__error">{{ remoteTargetInfo(id)?.error }}</p>
              <p v-else class="fleet-remote-target__hint">{{ $t('ui.fleet.remoteInboundWaiting') }}</p>
              <footer>
                {{ $t('ui.fleet.remoteInboundGeneratedName') }}: <strong>{{ remoteTargetTag(id) }}</strong>
              </footer>
            </article>
          </div>

          <v-alert v-if="remoteInboundPreviewResults.length" :type="remoteInboundStatusType" variant="tonal" density="compact">
            {{ remoteInboundStatusText }}
          </v-alert>
          <div v-if="remoteInboundPreviewResults.length" class="fleet-remote-inbound__results">
            <div v-for="result in remoteInboundPreviewResults" :key="result.id" class="fleet-remote-inbound__result" :class="result.success ? 'is-success' : 'is-error'">
              <div>
                <strong>{{ result.name }}</strong>
                <span>{{ result.tag }}</span>
                <small v-if="result.created">{{ $t('ui.fleet.remoteInboundCreated') }}</small>
                <small v-else-if="result.success">{{ $t('ui.fleet.remoteInboundTargetPreflightPassed') }}</small>
                <small v-else-if="result.error">{{ result.error }}</small>
                <small v-else-if="remoteInboundFailedChecks(result).length">{{ remoteInboundFailedChecks(result).join(' · ') }}</small>
              </div>
              <v-icon :icon="result.success ? 'mdi-check-circle' : 'mdi-alert-circle'" />
            </div>
          </div>
        </v-card-text>
        <v-card-actions class="fleet-remote-inbound__footer">
          <v-spacer />
          <v-btn variant="text" @click="showRemoteInbound = false">{{ $t('ui.common.close') }}</v-btn>
          <v-btn
            color="primary"
            :loading="remoteInboundLoading === 'preview'"
            :disabled="!canPreviewRemoteInbound || remoteInboundLoading !== '' || remoteInboundCreateAttempted"
            @click="previewRemoteInbound"
          ><v-icon icon="mdi-file-search-outline" start />{{ $t('ui.fleet.remoteInboundPreview') }}</v-btn>
          <v-btn
            v-if="remoteInboundPreviewPassed && !remoteInboundCreateAttempted"
            color="success"
            :loading="remoteInboundLoading === 'create'"
            :disabled="remoteInboundLoading !== ''"
            @click="createRemoteInbound"
          ><v-icon icon="mdi-check-circle" start />{{ $t('ui.fleet.remoteInboundCreateAction') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="showDetails" max-width="760" scrollable>
      <v-card rounded="xl" class="fleet-dialog" v-if="selectedServer">
        <v-card-title class="fleet-dialog__title">
          <span>{{ selectedServer.name }} · {{ $t('ui.fleet.serverDetails') }}</span>
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="showDetails = false" />
        </v-card-title>
        <v-card-text>
          <div class="fleet-detail__status">
            <v-chip :color="selectedServer.reachable ? 'success' : 'error'" variant="tonal">
              {{ selectedServer.reachable ? $t('ui.common.online') : $t('ui.fleet.unreachable') }}
            </v-chip>
            <span>{{ $t('ui.fleet.checkTime', { time: selectedServer.checkedAt ? new Date(selectedServer.checkedAt).toLocaleString() : '-' }) }}</span>
            <span>{{ $t('ui.common.latency') }}：{{ selectedServer.id === 'local' ? $t('ui.common.local') : `${selectedServer.latencyMs} ms` }}</span>
          </div>
          <v-alert v-if="updateStates[selectedServer.id]" class="mt-4" variant="tonal" :type="updateAlertType(selectedServer)">
            <strong>{{ $t('ui.fleet.updateStatus') }}</strong>{{ updateStateLabel(selectedServer) }}
            <span v-if="updateStates[selectedServer.id]?.message"> · {{ updateStates[selectedServer.id].message }}</span>
          </v-alert>
          <div class="fleet-detail__grid">
            <div><span>{{ $t('ui.common.cpu') }}</span><strong>{{ selectedServer.ResourcesReady ? formatPercent(selectedServer.CPUPercent) : '-' }}</strong></div>
            <div><span>{{ $t('ui.common.memory') }}</span><strong>{{ selectedServer.ResourcesReady ? formatMemory(selectedServer.MemoryUsed, selectedServer.MemoryTotal, true) : '-' }}</strong></div>
            <div><span>{{ $t('ui.common.uploadSpeed') }}</span><strong>{{ networkRateLabel(selectedServer, 'upload') }}</strong></div>
            <div><span>{{ $t('ui.common.downloadSpeed') }}</span><strong>{{ networkRateLabel(selectedServer, 'download') }}</strong></div>
            <div><span>{{ trafficTitle(selectedServer) }}</span><strong>{{ trafficTotalLabel(selectedServer) }}</strong></div>
            <div><span>{{ $t('ui.fleet.trafficBreakdown') }}</span><strong>{{ trafficDetailLabel(selectedServer) }}</strong></div>
            <div><span>{{ $t('ui.fleet.address') }}</span><strong>{{ selectedServer.url }}</strong></div>
            <div><span>{{ $t('ui.common.publicIp') }}</span><strong>{{ selectedServer.PublicIP || '-' }}</strong></div>
            <div><span>{{ $t('ui.common.version') }}</span><strong>{{ selectedServer.System?.appVersion || '-' }}</strong></div>
            <div><span>{{ $t('ui.common.uptime') }}</span><strong>{{ formatUptime(selectedServer.Uptime) }}</strong></div>
            <div><span>{{ $t('ui.common.firewall') }}</span><strong>{{ selectedServer.portBackend || '-' }}</strong></div>
            <div><span>{{ $t('ui.fleet.listenersNat') }}</span><strong>{{ selectedServer.listeners }} / {{ selectedServer.natRules }}</strong></div>
            <div><span>{{ $t('ui.fleet.usersOnline') }}</span><strong>{{ selectedServer.Clients }} / {{ selectedServer.OnlineUsers }}</strong></div>
            <div><span>{{ $t('ui.fleet.inboundsOutbounds') }}</span><strong>{{ selectedServer.Inbounds }} / {{ selectedServer.Outbounds }}</strong></div>
            <div><span>{{ $t('ui.common.endpoints') }}</span><strong>{{ selectedServer.Endpoints }}</strong></div>
            <div><span>MASQUE</span><strong>{{ selectedServer.MasqueRunning }} / {{ selectedServer.MasqueTotal }}</strong></div>
            <div><span>Mieru</span><strong>{{ selectedServer.MieruRunning }} / {{ selectedServer.MieruTotal }}</strong></div>
          </div>
          <FleetTrafficHistory
            v-if="selectedServer.TrafficBudget?.enabled"
            :history="trafficHistory"
            :loading="trafficHistoryLoading"
            :error="trafficHistoryError"
          />
          <section v-if="selectedServer.configuration" class="fleet-config-compare">
            <div class="fleet-detail__log-head">
              <span>{{ $t('ui.fleet.configSnapshot') }}</span>
              <v-chip size="small" :color="selectedServer.driftCount ? 'warning' : 'success'" variant="tonal">
                {{ selectedServer.id === 'local' ? $t('ui.fleet.baseline') : selectedServer.driftCount ? $t('ui.fleet.driftItems', { count: selectedServer.driftCount }) : $t('ui.fleet.sameAsLocal') }}
              </v-chip>
            </div>
            <div class="fleet-config-snapshot">
              <div><span>{{ $t('ui.fleet.panel') }}</span><strong>{{ selectedServer.configuration.webPort }} · {{ selectedServer.configuration.webPath }} · {{ tlsLabel(selectedServer.configuration.webTls) }}</strong></div>
              <div><span>{{ $t('ui.fleet.subscription') }}</span><strong>{{ selectedServer.configuration.subPort }} · {{ selectedServer.configuration.subPath }} · {{ tlsLabel(selectedServer.configuration.subTls) }}</strong></div>
              <div><span>{{ $t('ui.fleet.panelDomain') }}</span><strong>{{ selectedServer.configuration.webDomain || $t('ui.fleet.notSet') }}</strong></div>
              <div><span>{{ $t('ui.fleet.subscriptionDomain') }}</span><strong>{{ selectedServer.configuration.subDomain || $t('ui.fleet.notSet') }}</strong></div>
              <div><span>{{ $t('ui.fleet.subscriptionRole') }}</span><strong>{{ selectedServer.configuration.subMode === 'master' ? $t('ui.fleet.master') : $t('ui.fleet.slave') }}</strong></div>
              <div><span>{{ $t('ui.fleet.subscriptionOptions') }}</span><strong>Base64 {{ boolLabel(selectedServer.configuration.subEncode) }} · {{ $t('ui.fleet.userInfo') }} {{ boolLabel(selectedServer.configuration.subShowInfo) }}</strong></div>
            </div>
            <div v-if="selectedServer.drift?.length" class="fleet-drift-list">
              <div v-for="item in selectedServer.drift" :key="item.field" class="fleet-drift-item">
                <span>{{ item.label }}</span>
                <strong>{{ formatDriftValue(item.actual) }}</strong>
                <v-icon icon="mdi-arrow-left" size="16" />
                <small>{{ $t('ui.fleet.baseline') }} {{ formatDriftValue(item.expected) }}</small>
              </div>
            </div>
          </section>
          <v-alert v-else-if="selectedServer.reachable" type="info" variant="tonal" class="mt-4">{{ $t('ui.fleet.remoteConfigUnavailable') }}</v-alert>
          <v-alert v-if="selectedServer.error" type="error" variant="tonal" class="mt-4">{{ selectedServer.error }}</v-alert>
          <div class="fleet-detail__log-head">
            <span>{{ $t('ui.fleet.recentLogs') }}</span>
            <v-btn size="small" variant="tonal" :loading="logsLoading" @click="loadLogs(selectedServer)">{{ $t('ui.fleet.refreshLogs') }}</v-btn>
          </div>
          <pre class="fleet-detail__logs">{{ logLines.length ? logLines.join('\n') : $t('ui.fleet.logsHint') }}</pre>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showDetails = false">{{ $t('ui.common.close') }}</v-btn>
          <v-btn color="warning" :loading="actionLoading" :disabled="batchAction !== '' || !selectedServer.reachable" @click="restartServer(selectedServer)">
            <v-icon icon="mdi-restart" start />{{ $t('ui.fleet.restartPanel') }}
          </v-btn>
          <v-btn color="primary" :loading="updateLoadingId === selectedServer.id" :disabled="batchAction !== '' || !selectedServer.reachable" @click="updateServer(selectedServer)">
            <v-icon icon="mdi-download-outline" start />{{ $t('ui.fleet.backgroundUpdate') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'
import FleetTrafficHistory from '@/components/FleetTrafficHistory.vue'
import FleetInboundDraftEditor from '@/components/FleetInboundDraftEditor.vue'
import { createInbound, type Inbound } from '@/types/inbounds'
import {
  trafficBudgetCap as resolveTrafficBudgetCap,
  trafficBudgetPercent as resolveTrafficBudgetPercent,
  trafficBudgetReadable as isTrafficBudgetReadable,
  type FleetTrafficBudget,
  type TrafficBudgetHistory,
} from '@/utils/trafficBudget'

type FleetServer = {
  id: string
  name: string
  url: string
  enabled: boolean
  tokenSet?: boolean
  reachable: boolean
  latencyMs: number
  checkedAt?: string
  error?: string
  lastKnown?: boolean
  lastSuccessAt?: string
  System?: Record<string, any>
  Core?: Record<string, any>
  system?: Record<string, any>
  core?: Record<string, any>
  PublicIP: string
  Uptime: number
  CPUPercent: number
  MemoryUsed: number
  MemoryTotal: number
  NetworkSent: number
  NetworkReceived: number
  NetworkTotalsReady: boolean
  TrafficBudget?: FleetTrafficBudget
  UploadRate: number
  DownloadRate: number
  NetworkRateReady: boolean
  ResourcesReady: boolean
  OnlineUsers: number
  OnlineInbounds: number
  OnlineOutbounds: number
  Clients: number
  Inbounds: number
  Outbounds: number
  Endpoints: number
  MasqueTotal: number
  MasqueRunning: number
  MieruTotal: number
  MieruRunning: number
  portBackend?: string
  listeners: number
  natRules: number
  configuration?: FleetConfigProfile
  drift?: FleetConfigDrift[]
  driftCount: number
}

type FleetConfigProfile = {
  appVersion: string
  webPort: number
  webPath: string
  webTls: string
  webDomain?: string
  subPort: number
  subPath: string
  subTls: string
  subDomain?: string
  subMode: string
  subEncode: boolean
  subShowInfo: boolean
}

type FleetConfigDrift = { field: string; label: string; expected: unknown; actual: unknown }

type FleetConfig = {
  id: string
  name: string
  url: string
  token: string
  tokenSet?: boolean
  enabled: boolean
}

type FleetRemoteTLSProfile = { id: number; name: string; serverEnabled: boolean; serverName?: string; acmeDomains?: string[]; certificateFingerprint?: string }
type FleetRemoteInboundCapabilities = { hostname: string; subscriptionHost: string; tlsProfiles: FleetRemoteTLSProfile[]; inboundTags: string[] }
type FleetRemoteInboundTarget = { id: string; name: string; success: boolean; capabilities?: FleetRemoteInboundCapabilities; error?: string }
type FleetRemoteInboundCheck = { name: string; status: string; detail: string }
type FleetRemoteInboundResult = { id: string; name: string; tag?: string; success: boolean; created: boolean; error?: string; preview?: { valid: boolean; checks?: FleetRemoteInboundCheck[] } }
type FleetRemoteInboundTargetSettings = { tlsId: number | null; listenPort: number; tag: string; nameEdited: boolean }

const t = i18n.global.t
const remoteInboundTLSProtocols = ['http', 'vmess', 'trojan', 'naive', 'hysteria', 'tuic', 'hysteria2', 'vless', 'anytls']
const remoteInboundRequiredTLSProtocols = ['naive', 'hysteria', 'hysteria2', 'tuic', 'anytls']

const loading = ref(true)
const saving = ref(false)
const showConfig = ref(false)
const showDetails = ref(false)
const showRemoteInbound = ref(false)
const servers = ref<FleetServer[]>([])
const configs = ref<FleetConfig[]>([])
const checkedAt = ref('')
const selectedServer = ref<FleetServer | null>(null)
const trafficHistory = ref<TrafficBudgetHistory | null>(null)
const trafficHistoryLoading = ref(false)
const trafficHistoryError = ref('')
const logLines = ref<string[]>([])
const logsLoading = ref(false)
const actionLoading = ref(false)
const batchAction = ref<'' | 'update' | 'restart'>('')
const batchMessage = ref('')
const batchMessageType = ref<'info' | 'success' | 'warning' | 'error'>('info')
const remoteInboundDraft = ref<Inbound>(createInbound('socks', { id: 0, tag: '', listen: '::', listen_port: 0, addrs: [], out_json: {} }) as Inbound)
const remoteInboundTargetIds = ref<string[]>([])
const remoteInboundTargets = ref<FleetRemoteInboundTarget[]>([])
const remoteTargetSettings = ref<Record<string, FleetRemoteInboundTargetSettings>>({})
const remoteInboundPreviewResults = ref<FleetRemoteInboundResult[]>([])
const remoteInboundLoading = ref<'' | 'targets' | 'preview' | 'create'>('')
const remoteInboundCreateAttempted = ref(false)
let remoteTargetRequestRevision = 0
const updateLoadingId = ref('')
const refreshLoadingId = ref('')
const updateStates = ref<Record<string, any>>({})
const pendingTimers = new Set<number>()
const networkSamples = new Map<string, { sent: number; received: number; checkedAt: number }>()
let fleetRequestActive = false

const schedule = (callback: () => void, delay: number) => {
  const timer = window.setTimeout(() => {
    pendingTimers.delete(timer)
    callback()
  }, delay)
  pendingTimers.add(timer)
}

const normalizeServer = (server: any): FleetServer => {
  const checkedAt = new Date(server.checkedAt ?? Date.now()).getTime()
  const sent = Number(server.networkSent ?? server.NetworkSent ?? 0)
  const received = Number(server.networkReceived ?? server.NetworkReceived ?? 0)
  const previous = networkSamples.get(server.id)
  let uploadRate = 0
  let downloadRate = 0
  let rateReady = false
  const resourcesReady = Boolean(server.resourcesReady ?? server.ResourcesReady)
  const networkTotalsReady = Boolean(server.networkTotalsReady ?? server.NetworkTotalsReady ?? resourcesReady)
  if (server.reachable && networkTotalsReady && previous && checkedAt > previous.checkedAt && sent >= previous.sent && received >= previous.received) {
    const elapsedSeconds = (checkedAt - previous.checkedAt) / 1000
    uploadRate = (sent - previous.sent) / elapsedSeconds
    downloadRate = (received - previous.received) / elapsedSeconds
    rateReady = true
  }
  if (server.reachable && networkTotalsReady && Number.isFinite(checkedAt)) networkSamples.set(server.id, { sent, received, checkedAt })

  const budget = server.trafficBudget ?? server.TrafficBudget

  return {
    ...server,
    System: server.system ?? server.System ?? {},
    Core: server.core ?? server.Core ?? {},
    PublicIP: server.publicIp ?? server.PublicIP ?? '',
    Uptime: server.uptime ?? server.Uptime ?? 0,
    CPUPercent: Number(server.cpuPercent ?? server.CPUPercent ?? 0),
    MemoryUsed: Number(server.memoryUsed ?? server.MemoryUsed ?? 0),
    MemoryTotal: Number(server.memoryTotal ?? server.MemoryTotal ?? 0),
    NetworkSent: sent,
    NetworkReceived: received,
    NetworkTotalsReady: networkTotalsReady,
    TrafficBudget: budget && typeof budget === 'object' ? budget : undefined,
    UploadRate: uploadRate,
    DownloadRate: downloadRate,
    NetworkRateReady: rateReady,
    ResourcesReady: resourcesReady,
    OnlineUsers: server.onlineUsers ?? server.OnlineUsers ?? 0,
    OnlineInbounds: server.onlineInbounds ?? server.OnlineInbounds ?? 0,
    OnlineOutbounds: server.onlineOutbounds ?? server.OnlineOutbounds ?? 0,
    Clients: server.clients ?? server.Clients ?? 0,
    Inbounds: server.inbounds ?? server.Inbounds ?? 0,
    Outbounds: server.outbounds ?? server.Outbounds ?? 0,
    Endpoints: server.endpoints ?? server.Endpoints ?? 0,
    MasqueTotal: server.masqueTotal ?? server.MasqueTotal ?? 0,
    MasqueRunning: server.masqueRunning ?? server.MasqueRunning ?? 0,
    MieruTotal: server.mieruTotal ?? server.MieruTotal ?? 0,
    MieruRunning: server.mieruRunning ?? server.MieruRunning ?? 0,
    listeners: server.listeners ?? 0,
    natRules: server.natRules ?? 0,
    configuration: server.configuration,
    drift: Array.isArray(server.drift) ? server.drift : [],
    driftCount: server.driftCount ?? 0,
  }
}

const remoteServers = computed(() => servers.value.filter((server) => server.id !== 'local'))
const reachableCount = computed(() => servers.value.filter((server) => server.reachable).length)
const runningCount = computed(() => servers.value.filter((server) => server.Core?.running).length)
const errorCount = computed(() => servers.value.filter((server) => server.error || !server.reachable).length)
const remoteCount = computed(() => remoteServers.value.length)
const onlineUsersTotal = computed(() => servers.value.reduce((total, server) => total + server.OnlineUsers, 0))
const endpointTotal = computed(() => servers.value.reduce((total, server) => total + server.Endpoints, 0))
const driftServerCount = computed(() => servers.value.filter((server) => server.driftCount > 0).length)
const initialLoading = computed(() => loading.value && servers.value.length === 0)
const remoteInboundServers = computed(() => remoteServers.value.filter(server => server.reachable && server.enabled))
const remoteInboundTLSCapable = computed(() => remoteInboundTLSProtocols.includes(remoteInboundDraft.value.type))
const remoteInboundTLSRequired = computed(() => remoteInboundRequiredTLSProtocols.includes(remoteInboundDraft.value.type)
  || (remoteInboundDraft.value.type === 'http' && Number((remoteInboundDraft.value as any).version) >= 2))
const remoteInboundPreviewPassed = computed(() => Boolean(
  remoteInboundTargetIds.value.length > 0
  && remoteInboundPreviewResults.value.length === remoteInboundTargetIds.value.length
  && remoteInboundPreviewResults.value.every(result => result.success),
))
const remoteInboundCreatedCount = computed(() => remoteInboundPreviewResults.value.filter(result => result.created).length)
const remoteInboundCreationComplete = computed(() => Boolean(
  remoteInboundCreateAttempted.value
  && remoteInboundPreviewResults.value.length === remoteInboundTargetIds.value.length
  && remoteInboundPreviewResults.value.length > 0
  && remoteInboundPreviewResults.value.every(result => result.success && result.created),
))
const remoteInboundStatusType = computed(() => {
  if (!remoteInboundCreateAttempted.value) return remoteInboundPreviewPassed.value ? 'success' : 'error'
  if (remoteInboundLoading.value === 'create') return 'info'
  if (remoteInboundCreationComplete.value) return 'success'
  return remoteInboundCreatedCount.value > 0 ? 'warning' : 'error'
})
const remoteInboundStatusText = computed(() => {
  if (!remoteInboundCreateAttempted.value) {
    return remoteInboundPreviewPassed.value ? t('ui.fleet.remoteInboundPreviewPassed') : t('ui.fleet.remoteInboundPreviewFailed')
  }
  if (remoteInboundLoading.value === 'create') return t('ui.fleet.remoteInboundCreating')
  if (remoteInboundCreationComplete.value) return t('ui.fleet.remoteInboundCreateComplete')
  return remoteInboundCreatedCount.value > 0
    ? t('ui.fleet.remoteInboundCreatePartial')
    : t('ui.fleet.remoteInboundCreateFailedSummary')
})
const canPreviewRemoteInbound = computed(() => Boolean(
  remoteInboundTargetIds.value.length > 0
  && remoteInboundTargets.value.length === remoteInboundTargetIds.value.length
  && remoteInboundTargets.value.every(target => target.success)
  && remoteInboundTargetIds.value.every(id => {
    const settings = remoteTargetSettings.value[id]
    return Boolean(settings && !remoteInboundTagError(id) && settings.listenPort >= 1 && settings.listenPort <= 65535
      && (!remoteInboundTLSRequired.value || (settings.tlsId !== null && settings.tlsId > 0)))
  }),
))
const formattedCheckedAt = computed(() => {
  if (!checkedAt.value) return '-'
  const date = new Date(checkedAt.value)
  return Number.isNaN(date.getTime()) ? checkedAt.value : date.toLocaleString()
})

const formatUptime = (seconds: number) => {
  if (!seconds || seconds < 1) return '-'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return t('ui.fleet.daysHours', { days, hours })
  if (hours > 0) return t('ui.fleet.hoursMinutes', { hours, minutes })
  return t('ui.fleet.minutes', { minutes: Math.max(minutes, 1) })
}

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(value >= 10 ? 0 : 1)}%`
const memoryPercent = (server: FleetServer) => server.MemoryTotal > 0 ? clampPercent((server.MemoryUsed / server.MemoryTotal) * 100) : 0
const formatBytes = (value: number, suffix = '') => {
  if (!Number.isFinite(value) || value < 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  const digits = size >= 100 || unitIndex === 0 ? 0 : size >= 10 ? 1 : 2
  return `${size.toFixed(digits)} ${units[unitIndex]}${suffix}`
}
const formatMemory = (used: number, total: number, detailed = false) => {
  if (!total) return '-'
  const percent = formatPercent((used / total) * 100)
  return detailed ? `${formatBytes(used)} / ${formatBytes(total)} · ${percent}` : percent
}
const networkRateLabel = (server: FleetServer, direction: 'upload' | 'download') => {
  if (!server.reachable || !server.NetworkTotalsReady) return '-'
  if (!server.NetworkRateReady) return t('ui.common.sampling')
  return formatBytes(direction === 'upload' ? server.UploadRate : server.DownloadRate, '/s')
}

const formatTrafficBytes = (value: number) => {
  if (!Number.isFinite(value) || value < 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = value
  let unitIndex = 0
  while (size >= 1000 && unitIndex < units.length - 1) {
    size /= 1000
    unitIndex += 1
  }
  const digits = size >= 100 || unitIndex === 0 ? 0 : size >= 10 ? 1 : 2
  return `${size.toFixed(digits)} ${units[unitIndex]}`
}
const trafficTitle = (server: FleetServer) => t(server.TrafficBudget?.enabled ? 'ui.fleet.cycleTraffic' : 'ui.fleet.bootTraffic')
const trafficBudgetCap = (server: FleetServer) => resolveTrafficBudgetCap(server.TrafficBudget)
const trafficBudgetReadable = (server: FleetServer) => isTrafficBudgetReadable(server.TrafficBudget)
const trafficBudgetPercent = (server: FleetServer) => resolveTrafficBudgetPercent(server.TrafficBudget)
const trafficBudgetColor = (server: FleetServer) => {
  const budget = server.TrafficBudget
  if (budget?.blocked || budget?.level === 'error' || budget?.level === 'critical') return 'error'
  return budget?.level === 'warning' ? 'warning' : 'success'
}
const trafficBudgetStatusLabel = (server: FleetServer) => {
  const budget = server.TrafficBudget
  const percent = trafficBudgetPercent(server).toFixed(1)
  const state = budget?.blocked
    ? t('ui.trafficBudget.statusBlocked')
    : budget?.level === 'critical'
      ? t('ui.trafficBudget.statusCritical')
      : budget?.level === 'warning'
        ? t('ui.trafficBudget.statusWarning')
        : t('ui.trafficBudget.statusNormal')
  return `${percent}% · ${state}`
}
const trafficTotalLabel = (server: FleetServer) => {
  if (!server.reachable && !server.lastKnown) return '-'
  const budget = server.TrafficBudget
  if (budget?.enabled) {
    if (!trafficBudgetReadable(server)) return '-'
    return `${formatTrafficBytes(Number(budget.usedBytes))} / ${formatTrafficBytes(trafficBudgetCap(server))}`
  }
  if (!server.NetworkTotalsReady) return '-'
  return formatTrafficBytes(server.NetworkSent + server.NetworkReceived)
}
const trafficDetailLabel = (server: FleetServer) => {
  if (!server.reachable && !server.lastKnown) return t('ui.fleet.trafficUnavailable')
  const budget = server.TrafficBudget
  const lastSample = server.lastSuccessAt ? new Date(server.lastSuccessAt) : null
  const lastKnown = server.lastKnown && lastSample && Number.isFinite(lastSample.getTime())
    ? ` · ${t('ui.fleet.trafficLastSample', { time: lastSample.toLocaleString() })}`
    : ''
  if (budget?.enabled && (!budget.supported || budget.level === 'error' || budget.error)) return `${budget.error || t('ui.fleet.trafficUnavailable')}${lastKnown}`
  if (budget?.enabled) {
    const mode = t(`ui.fleet.trafficMode.${budget.accountingMode || 'tx'}`)
    const baseline = Number(budget.offsetBytes) > 0 ? ` · ${t('ui.fleet.trafficBaseline')} ${formatTrafficBytes(Number(budget.offsetBytes))}` : ''
    const reserve = Number(budget.reserveBytes) > 0 ? ` · ${t('ui.trafficBudget.reserve')} ${formatTrafficBytes(Number(budget.reserveBytes))}` : ''
    return `${mode} · TX ${formatTrafficBytes(Number(budget.meteredTxBytes))} · RX ${formatTrafficBytes(Number(budget.meteredRxBytes))}${reserve}${baseline}${lastKnown}`
  }
  if (!server.NetworkTotalsReady) return t('ui.fleet.trafficUnavailable')
  return `TX ${formatTrafficBytes(server.NetworkSent)} · RX ${formatTrafficBytes(server.NetworkReceived)}${lastKnown}`
}

const tlsLabel = (state: string) => ({ enabled: t('ui.fleet.tlsEnabled'), disabled: t('ui.fleet.tlsDisabled'), partial: t('ui.fleet.tlsPartial') } as Record<string, string>)[state] ?? state
const boolLabel = (value: boolean) => value ? t('ui.fleet.on') : t('ui.fleet.off')
const formatDriftValue = (value: unknown) => {
  if (typeof value === 'boolean') return boolLabel(value)
  return String(value ?? '-')
}

const loadFleet = async (silent = false) => {
  if (fleetRequestActive) return
  fleetRequestActive = true
  if (!silent) loading.value = true
  try {
    const response = await HttpUtils.get('api/fleet')
    if (response.success && response.obj) {
      const nextServers = (response.obj.servers ?? []).map(normalizeServer)
      servers.value = nextServers
      checkedAt.value = response.obj.checkedAt ?? ''
      if (!showConfig.value) configs.value = nextServers.filter((server: FleetServer) => server.id !== 'local').map((server: FleetServer) => ({
        id: server.id,
        name: server.name,
        url: server.url,
        token: '',
        tokenSet: server.tokenSet,
        enabled: server.enabled,
      }))
      if (selectedServer.value) selectedServer.value = nextServers.find((server: FleetServer) => server.id === selectedServer.value?.id) ?? selectedServer.value
    }
  } finally {
    if (!silent) loading.value = false
    fleetRequestActive = false
  }
}

const pollFleet = async () => {
  if (document.visibilityState === 'visible' && !showConfig.value && !batchAction.value) await loadFleet(true)
  schedule(pollFleet, 5000)
}

const addConfig = () => {
  configs.value.push({ id: '', name: '', url: '', token: '', enabled: true })
}

const removeConfig = (index: number) => {
  configs.value.splice(index, 1)
}

const saveConfig = async () => {
  saving.value = true
  const response = await HttpUtils.post('api/fleetSave', {
    data: JSON.stringify(configs.value.map(({ id, name, url, token, enabled }) => ({ id, name, url, token, enabled }))),
  })
  if (response.success) {
    showConfig.value = false
    await loadFleet()
  }
  saving.value = false
}

const openRemoteInboundDialog = async () => {
  showRemoteInbound.value = true
  remoteInboundDraft.value = createInbound('socks', { id: 0, tag: '', listen: '::', listen_port: 0, addrs: [], out_json: {} }) as Inbound
  remoteInboundTargetIds.value = []
  remoteInboundTargets.value = []
  remoteTargetSettings.value = {}
  remoteInboundPreviewResults.value = []
  remoteInboundCreateAttempted.value = false
}

const clearRemoteInboundPreview = () => {
  remoteInboundPreviewResults.value = []
  remoteInboundCreateAttempted.value = false
}

const resetRemoteInboundTargetSettings = () => {
  const nextSettings: Record<string, FleetRemoteInboundTargetSettings> = {}
  for (const target of remoteInboundTargets.value) {
    const profiles = (target.capabilities?.tlsProfiles ?? []).filter(profile => profile.serverEnabled)
    const previous = remoteTargetSettings.value[target.id]
    const selectedTLSID = profiles.some(profile => profile.id === previous?.tlsId) ? previous?.tlsId ?? null : null
    nextSettings[target.id] = {
      listenPort: previous?.listenPort ?? Math.floor(Math.random() * 40000) + 20000,
      tlsId: selectedTLSID,
      tag: previous?.nameEdited ? previous.tag : remoteGeneratedTag(target.id),
      nameEdited: previous?.nameEdited ?? false,
    }
  }
  remoteTargetSettings.value = nextSettings
  clearRemoteInboundPreview()
}

watch(() => [remoteInboundDraft.value.type, (remoteInboundDraft.value as any).version], () => {
  for (const target of remoteInboundTargets.value) {
    const settings = remoteTargetSettings.value[target.id]
    if (!settings) continue
    if (!settings.nameEdited) settings.tag = remoteGeneratedTag(target.id)
    const profiles = (target.capabilities?.tlsProfiles ?? []).filter(profile => profile.serverEnabled)
    if (!remoteInboundTLSCapable.value || remoteInboundDraft.value.type === 'masque') {
      settings.tlsId = null
    } else if (settings.tlsId !== null && !profiles.some(profile => profile.id === settings.tlsId)) {
      settings.tlsId = null
    }
  }
  clearRemoteInboundPreview()
})

watch(remoteInboundDraft, clearRemoteInboundPreview, { deep: true })

const toggleRemoteInboundTarget = async (id: string) => {
  const selected = remoteInboundTargetIds.value.includes(id)
    ? remoteInboundTargetIds.value.filter(target => target !== id)
    : [...remoteInboundTargetIds.value, id]
  remoteInboundTargetIds.value = selected
  remoteInboundTargets.value = []
  clearRemoteInboundPreview()
  const requestRevision = ++remoteTargetRequestRevision
  if (!selected.length) {
    remoteTargetSettings.value = {}
    remoteInboundLoading.value = ''
    return
  }

  remoteInboundLoading.value = 'targets'
  const response = await HttpUtils.get('api/fleetInboundTargets', { targets: JSON.stringify(selected) })
  if (requestRevision !== remoteTargetRequestRevision) return
  remoteInboundLoading.value = ''
  if (!response.success || !Array.isArray(response.obj)) {
    remoteInboundTargets.value = selected.map(targetID => ({ id: targetID, name: remoteInboundServer(targetID)?.name ?? targetID, success: false, error: response.msg || t('ui.fleet.remoteInboundTargetsFailed') }))
    return
  }
  const byID = new Map((response.obj as FleetRemoteInboundTarget[]).map(target => [target.id, target]))
  remoteInboundTargets.value = selected.map(targetID => byID.get(targetID) ?? ({
    id: targetID,
    name: remoteInboundServer(targetID)?.name ?? targetID,
    success: false,
    error: t('ui.fleet.remoteInboundTargetMissing'),
  }))
  resetRemoteInboundTargetSettings()
}

const remoteTargetInfo = (id: string) => remoteInboundTargets.value.find(target => target.id === id)
const remoteInboundServer = (id: string) => remoteServers.value.find(server => server.id === id)
const remoteTagSlug = (value: string) => value.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
const remoteGeneratedTag = (id: string) => {
  const server = remoteInboundServer(id)
  let serverPart = remoteTagSlug(server?.name ?? '') || remoteTagSlug(id)
  const matchingServers = remoteServers.value.filter(candidate =>
    (remoteTagSlug(candidate.name) || remoteTagSlug(candidate.id)) === serverPart,
  )
  if (matchingServers.length > 1) serverPart += `-${remoteTagSlug(id)}`
  const protocol = remoteInboundDraft.value.type === 'hysteria' ? 'hy'
    : remoteInboundDraft.value.type === 'hysteria2' ? 'hy2'
      : remoteInboundDraft.value.type === 'shadowsocks' ? 'ss'
        : remoteTagSlug(remoteInboundDraft.value.type)
  const base = `${serverPart}-${protocol}`
  const used = new Set((remoteTargetInfo(id)?.capabilities?.inboundTags ?? []).map(tag => tag.trim().toLowerCase()))
  if (!used.has(base.toLowerCase())) return base
  let suffix = 2
  while (used.has(`${base}-${suffix}`.toLowerCase())) suffix++
  return `${base}-${suffix}`
}
const remoteTargetTag = (id: string) => remoteTargetSettings.value[id]?.tag.trim() ?? remoteGeneratedTag(id)
const remoteInboundTagError = (id: string) => {
  const tag = remoteTargetTag(id).toLowerCase()
  if (!tag) return t('ui.fleet.remoteInboundNameRequired')
  if ((remoteTargetInfo(id)?.capabilities?.inboundTags ?? []).some(existing => existing.trim().toLowerCase() === tag)) {
    return t('ui.fleet.remoteInboundNameTaken')
  }
  if (remoteInboundTargetIds.value.some(other => other !== id && remoteTargetTag(other).toLowerCase() === tag)) {
    return t('ui.fleet.remoteInboundNameDuplicate')
  }
  return ''
}
const updateRemoteInboundTag = (id: string, value: string | null) => {
  const settings = remoteTargetSettings.value[id]
  if (!settings) return
  settings.tag = value ?? ''
  settings.nameEdited = settings.tag !== remoteGeneratedTag(id)
  clearRemoteInboundPreview()
}
const remoteTlsItems = (id: string) => (remoteTargetInfo(id)?.capabilities?.tlsProfiles ?? []).filter(profile => profile.serverEnabled).map(profile => ({
  title: profile.serverName ? `${profile.name} · ${profile.serverName}` : profile.name,
  value: profile.id,
}))
const remoteTargetTlsSummary = (id: string) => {
  const settings = remoteTargetSettings.value[id]
  const profile = remoteTargetInfo(id)?.capabilities?.tlsProfiles?.find(item => item.id === settings?.tlsId)
  if (!profile) return t('ui.fleet.remoteInboundTLSSelect')
  const parts = [profile.serverName, profile.acmeDomains?.join(', '), profile.certificateFingerprint ? `SHA-256 ${profile.certificateFingerprint.slice(0, 16)}…` : ''].filter(Boolean)
  return parts.join(' · ') || profile.name
}

const remoteInboundFailedChecks = (result: FleetRemoteInboundResult) => (result.preview?.checks ?? [])
  .filter(check => check.status !== 'ok')
  .map(check => `${check.name}: ${check.detail}`)

const previewRemoteInbound = async () => {
  if (!canPreviewRemoteInbound.value) return
  remoteInboundLoading.value = 'preview'
  remoteInboundPreviewResults.value = []
  const plan = {
    inboundDraft: remoteInboundDraft.value,
    targets: remoteInboundTargetIds.value.map(id => ({
      id,
      tag: remoteTargetTag(id),
      tlsId: remoteTargetSettings.value[id]?.tlsId ?? 0,
      listenPort: remoteTargetSettings.value[id]?.listenPort ?? 0,
    })),
  }
  const response = await HttpUtils.post('api/fleetInboundPreview', { plan: JSON.stringify(plan) })
  if (response.success && Array.isArray(response.obj)) remoteInboundPreviewResults.value = response.obj
  else if (!response.success) remoteInboundPreviewResults.value = remoteInboundTargetIds.value.map(id => ({
    id,
    name: remoteInboundServer(id)?.name ?? id,
    tag: remoteTargetTag(id),
    success: false,
    created: false,
    error: response.msg || t('ui.fleet.remoteInboundTargetsFailed'),
  }))
  remoteInboundLoading.value = ''
}

const createRemoteInbound = async () => {
  if (!remoteInboundPreviewPassed.value || remoteInboundCreateAttempted.value) return
  const names = remoteInboundPreviewResults.value.map(result => `${result.name}: ${result.tag}`).join('\n')
  if (!window.confirm(t('ui.fleet.remoteInboundConfirm', { targets: names }))) return
  remoteInboundCreateAttempted.value = true
  remoteInboundLoading.value = 'create'
  const plan = {
    inboundDraft: remoteInboundDraft.value,
    targets: remoteInboundTargetIds.value.map(id => ({
      id,
      tag: remoteTargetTag(id),
      tlsId: remoteTargetSettings.value[id]?.tlsId ?? 0,
      listenPort: remoteTargetSettings.value[id]?.listenPort ?? 0,
    })),
  }
  const response = await HttpUtils.post('api/fleetInboundCreate', { plan: JSON.stringify(plan) })
  if (response.success && Array.isArray(response.obj)) {
    remoteInboundPreviewResults.value = response.obj
    remoteInboundLoading.value = ''
    await loadFleet(true)
  } else {
    remoteInboundPreviewResults.value = remoteInboundTargetIds.value.map(id => ({
      id,
      name: remoteInboundServer(id)?.name ?? id,
      tag: remoteTargetTag(id),
      success: false,
      created: false,
      error: `${response.msg || t('ui.fleet.remoteInboundCreateFailed')} · ${t('ui.fleet.remoteInboundVerifyBeforeRetry')}`,
    }))
    remoteInboundLoading.value = ''
  }
}

const openDetails = (server: FleetServer) => {
  selectedServer.value = server
  logLines.value = []
  trafficHistory.value = null
  trafficHistoryError.value = ''
  showDetails.value = true
  void loadUpdateStatus(server)
  if (server.TrafficBudget?.enabled) void loadTrafficHistory(server)
}

const loadTrafficHistory = async (server: FleetServer) => {
  trafficHistoryLoading.value = true
  trafficHistoryError.value = ''
  try {
    const response = await HttpUtils.get('api/fleetTrafficHistory', { id: server.id })
    if (response.success && response.obj) {
      trafficHistory.value = response.obj as TrafficBudgetHistory
    } else {
      trafficHistory.value = null
      trafficHistoryError.value = response.msg || t('ui.fleet.trafficHistoryUnavailable')
    }
  } catch {
    trafficHistory.value = null
    trafficHistoryError.value = t('ui.fleet.trafficHistoryUnavailable')
  } finally {
    trafficHistoryLoading.value = false
  }
}

const loadUpdateStatus = async (server: FleetServer) => {
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'update-status' })
  if (response.success) {
    updateStates.value = { ...updateStates.value, [server.id]: response.obj ?? {} }
  }
}

const refreshServer = async (server: FleetServer) => {
  refreshLoadingId.value = server.id
  const response = await HttpUtils.post('api/fleetRefresh', { id: server.id })
  if (response.success && response.obj) {
    const refreshed = normalizeServer(response.obj)
    servers.value = servers.value.map((item) => item.id === refreshed.id ? refreshed : item)
    if (selectedServer.value?.id === refreshed.id) selectedServer.value = refreshed
  }
  refreshLoadingId.value = ''
}

const loadLogs = async (server: FleetServer) => {
  logsLoading.value = true
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'logs' })
  if (response.success) {
    logLines.value = Array.isArray(response.obj) ? response.obj.map((line) => String(line)) : []
  }
  logsLoading.value = false
}

const openLogs = async (server: FleetServer) => {
  openDetails(server)
  await loadLogs(server)
}

const runBatchAction = async (action: 'update' | 'restart') => {
  if (batchAction.value) return
  const remoteTargets = remoteServers.value.filter((server) => server.reachable && server.enabled)
  const localTarget = servers.value.find((server) => server.id === 'local' && server.reachable)
  const targets = [...remoteTargets, ...(localTarget ? [localTarget] : [])]
  if (!targets.length) {
    batchMessageType.value = 'warning'
    batchMessage.value = t('ui.fleet.noTargets')
    return
  }
  const actionLabel = action === 'update' ? t('ui.common.update') : t('ui.common.restart')
  const targetNames = targets.map((server) => server.name).join('、')
  if (!window.confirm(t('ui.fleet.confirmBatch', { action: actionLabel, targets: targetNames }))) return

  batchAction.value = action
  batchMessageType.value = 'info'
  batchMessage.value = t('ui.fleet.preparing', { action: actionLabel, count: targets.length })
  let remoteFailed = false
  let localFailed = false
  const failedRemoteNames: string[] = []

  for (let index = 0; index < targets.length; index += 1) {
    const server = targets[index]
    if (server.id === 'local' && remoteFailed) break
    batchMessage.value = t('ui.fleet.runningAction', { action: actionLabel, name: server.name, current: index + 1, total: targets.length })
    const response = await HttpUtils.post('api/fleetAction', { id: server.id, action })
    if (!response.success) {
      if (server.id !== 'local') {
        remoteFailed = true
        failedRemoteNames.push(server.name)
      }
      else localFailed = true
      batchMessageType.value = 'error'
      batchMessage.value = t('ui.fleet.actionFailed', { name: server.name, action: actionLabel, message: response.msg })
      if (server.id !== 'local') continue
      break
    }
  }

  if (remoteFailed) {
    batchAction.value = ''
    batchMessageType.value = 'warning'
    batchMessage.value = t('ui.fleet.remoteFailed', { names: failedRemoteNames.join(', '), action: actionLabel })
    return
  }

  if (localFailed) {
    batchAction.value = ''
    return
  }

  batchAction.value = ''
  batchMessageType.value = 'success'
  batchMessage.value = localTarget ? t('ui.fleet.batchDoneWithLocal') : t('ui.fleet.batchDoneRemote')
  if (localTarget) {
    schedule(loadFleet, 4500)
  }
}

const restartServer = async (server: FleetServer) => {
  actionLoading.value = true
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'restart' })
  if (response.success) {
    showDetails.value = false
    schedule(loadFleet, 4500)
  }
  actionLoading.value = false
}

const updateServer = async (server: FleetServer) => {
  updateLoadingId.value = server.id
  const response = await HttpUtils.post('api/fleetAction', { id: server.id, action: 'update' })
  if (response.success) {
    updateStates.value = { ...updateStates.value, [server.id]: response.obj ?? { state: 'queued' } }
    schedule(() => loadUpdateStatus(server), 1500)
    schedule(loadFleet, 4500)
  }
  updateLoadingId.value = ''
}

const updateStateLabel = (server: FleetServer) => {
  const state = updateStates.value[server.id]?.state
  return ({ queued: t('ui.fleet.queued'), running: t('ui.fleet.updating'), success: t('ui.fleet.completed'), failed: t('ui.fleet.failed'), never: t('ui.fleet.never') } as Record<string, string>)[state] ?? state ?? t('ui.fleet.unknown')
}

const updateAlertType = (server: FleetServer) => {
  const state = updateStates.value[server.id]?.state
  if (state === 'failed') return 'error'
  if (state === 'success') return 'success'
  return 'info'
}

const statusClass = (server: FleetServer) => {
  if (!server.enabled) return 'is-disabled'
  if (!server.reachable) return 'is-error'
  return 'is-online'
}

onMounted(async () => {
  await loadFleet()
  schedule(pollFleet, 5000)
})
onBeforeUnmount(() => {
  pendingTimers.forEach(timer => window.clearTimeout(timer))
  pendingTimers.clear()
})
</script>

<style scoped lang="scss">
.fleet-shell {
  position: relative;
  max-width: 100%;
  min-width: 0;
  overflow: hidden;
}

.fleet-shell__inner {
  position: relative;
  z-index: 1;
  width: min(1500px, 100%);
  min-width: 0;
  margin: 0 auto;
  display: grid;
  gap: 18px;
}

.fleet-shell__inner > * { min-width: 0; }

.fleet-shell__glow {
  position: absolute;
  width: 360px;
  height: 360px;
  border-radius: 50%;
  filter: blur(80px);
  opacity: 0.22;
  pointer-events: none;
}

.fleet-shell__glow--one { background: #38bdf8; top: 0; right: 4%; }
.fleet-shell__glow--two { background: #2563eb; bottom: 10%; left: 10%; }

.fleet-hero,
.fleet-card,
.fleet-summary__card,
.fleet-dialog {
  min-width: 0;
  max-width: 100%;
  border: 1px solid var(--np-border);
  background: var(--np-surface) !important;
  box-shadow: var(--np-shadow-soft);
  backdrop-filter: blur(22px) saturate(1.12);
}

.fleet-hero { padding: 22px 24px 24px; }
.fleet-hero__topline { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; min-width: 0; }
.fleet-hero__badge { max-width: 100%; border: 1px solid rgba(10, 132, 255, 0.18); border-radius: 999px; padding: 6px 11px; color: var(--np-accent); overflow-wrap: anywhere; font-size: 0.75rem; font-weight: 700; letter-spacing: 0.08em; }
.fleet-hero__badge--soft { color: var(--np-text-muted); border-color: var(--np-border); letter-spacing: 0; }
.fleet-hero__content { min-width: 0; margin-top: 12px; }
.fleet-hero__content > .v-col { min-width: 0; }
.fleet-hero__title-row { display: flex; align-items: center; gap: 14px; }
.fleet-hero__icon, .fleet-card__icon { display: grid; place-items: center; border-radius: 18px; color: var(--np-accent); background: rgba(10, 132, 255, 0.12); }
.fleet-hero__icon { width: 58px; height: 58px; }
.fleet-hero__title { margin: 0; font-size: clamp(1.65rem, 3vw, 2.45rem); letter-spacing: -0.05em; }
.fleet-hero__subtitle { margin: 6px 0 0; color: var(--np-text-muted); }
.fleet-hero__meta { display: flex; gap: 10px; margin-top: 18px; color: var(--np-text-muted); font-size: 0.8rem; }
.fleet-hero__actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-content: center; gap: 10px; width: 100%; min-width: 0; max-width: 100%; }
.fleet-hero__actions .v-btn { width: 100%; min-width: 0; max-width: 100%; padding-inline: 10px; }
.fleet-hero__actions .v-btn :deep(.v-btn__content) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-batch-alert { width: 100%; min-width: 0; max-width: 100%; margin-top: 16px; overflow: hidden; }
.fleet-batch-alert :deep(.v-alert__content) { min-width: 0; overflow-wrap: anywhere; word-break: break-word; }

.fleet-summary__card { padding: 16px 18px; min-height: 92px; }
.fleet-summary { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 8px; margin: 0; }
.fleet-summary__col { flex: none; width: auto; max-width: none; padding: 0; }
.fleet-summary__label { color: var(--np-text-muted); font-size: 0.78rem; }
.fleet-summary__value { margin-top: 7px; font-size: 1.8rem; font-weight: 800; }
.fleet-summary__card--one { border-top: 3px solid #38bdf8; }
.fleet-summary__card--two { border-top: 3px solid #22c55e; }
.fleet-summary__card--three { border-top: 3px solid #a78bfa; }
.fleet-summary__card--four { border-top: 3px solid #fb7185; }
.fleet-summary__card--five { border-top: 3px solid #f59e0b; }
.fleet-summary__card--six { border-top: 3px solid #14b8a6; }
.fleet-summary__card--seven { border-top: 3px solid #f97316; }
.fleet-empty { border: 1px solid var(--np-border); }

.fleet-card { padding: 18px; height: 100%; }
.fleet-card__header, .fleet-card__identity, .fleet-card__footer { display: flex; align-items: center; }
.fleet-card__header { justify-content: space-between; gap: 12px; }
.fleet-card__chips { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.fleet-card__identity { gap: 12px; min-width: 0; }
.fleet-card__icon { width: 44px; height: 44px; flex: 0 0 auto; }
.fleet-card__icon.is-online { color: #22c55e; background: rgba(34, 197, 94, 0.12); }
.fleet-card__icon.is-error { color: #fb7185; background: rgba(251, 113, 133, 0.12); }
.fleet-card__icon.is-disabled { color: var(--np-text-muted); background: var(--np-surface-muted); }
.fleet-card__name-wrap { min-width: 0; }
.fleet-card__name { font-size: 1.05rem; font-weight: 750; }
.fleet-card__url { overflow: hidden; color: var(--np-text-muted); font-size: 0.76rem; text-overflow: ellipsis; white-space: nowrap; }
.fleet-monitor { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; padding: 14px 0 2px; }
.fleet-monitor__item { min-width: 0; padding: 10px 11px; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); }
.fleet-monitor__item > span, .fleet-monitor__head span { color: var(--np-text-muted); font-size: 0.7rem; }
.fleet-monitor__item > strong { display: block; margin-top: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.88rem; }
.fleet-monitor__head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 7px; }
.fleet-monitor__head strong { font-size: 0.82rem; }
.fleet-monitor__item--upload { box-shadow: inset 0 2px 0 rgba(245, 158, 11, .6); }
.fleet-monitor__item--download { box-shadow: inset 0 2px 0 rgba(34, 197, 94, .6); }
.fleet-traffic { display: grid; gap: 8px; margin-top: 12px; padding: 13px 14px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface-muted); }
.fleet-traffic__head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; min-width: 0; }
.fleet-traffic__title { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; min-width: 0; }
.fleet-traffic__head span, .fleet-traffic small { color: var(--np-text-muted); font-size: 0.72rem; }
.fleet-traffic__head strong { overflow: hidden; text-align: right; text-overflow: ellipsis; white-space: nowrap; font-size: 0.95rem; }
.fleet-traffic small { overflow-wrap: anywhere; line-height: 1.4; }
.fleet-card__metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 18px 0; }
.fleet-metric { display: grid; gap: 3px; }
.fleet-metric span { color: var(--np-text-muted); font-size: 0.75rem; }
.fleet-metric strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.93rem; }
.fleet-card__footer { justify-content: space-between; gap: 10px; min-width: 0; color: var(--np-text-muted); font-size: 0.8rem; }
.fleet-core-state { display: inline-flex; align-items: center; gap: 5px; }
.fleet-core-state.is-running { color: #22c55e; }
.fleet-card__error { flex: 1 1 auto; min-width: 0; overflow: hidden; color: #fb7185; text-overflow: ellipsis; white-space: nowrap; }
.fleet-card__actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--np-border); }
.fleet-detail__status { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; color: var(--np-text-muted); font-size: 0.82rem; }
.fleet-detail__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 18px; }
.fleet-detail__grid > div { display: grid; gap: 4px; padding: 12px; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); }
.fleet-detail__grid span, .fleet-detail__log-head { color: var(--np-text-muted); font-size: 0.76rem; }
.fleet-detail__grid strong { overflow-wrap: anywhere; }
.fleet-detail__log-head { display: flex; align-items: center; justify-content: space-between; margin-top: 18px; }
.fleet-detail__logs { max-height: 260px; margin: 8px 0 0; padding: 14px; overflow: auto; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); color: var(--np-text); white-space: pre-wrap; word-break: break-word; font: 0.76rem/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
.fleet-config-compare { margin-top: 18px; }
.fleet-config-snapshot { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 8px; }
.fleet-config-snapshot > div { display: grid; gap: 4px; padding: 11px 12px; border: 1px solid var(--np-border); border-radius: 14px; background: var(--np-surface-muted); }
.fleet-config-snapshot span, .fleet-drift-item span, .fleet-drift-item small { color: var(--np-text-muted); font-size: 0.75rem; }
.fleet-config-snapshot strong { overflow-wrap: anywhere; font-size: 0.86rem; }
.fleet-drift-list { display: grid; gap: 8px; margin-top: 10px; }
.fleet-drift-item { display: grid; grid-template-columns: minmax(110px, .8fr) minmax(90px, 1fr) auto minmax(120px, 1fr); align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid rgba(249, 115, 22, .22); border-radius: 13px; background: rgba(249, 115, 22, .07); }

@media (max-width: 600px) {
  .fleet-hero__content { margin-inline: 0; }
  .fleet-hero__content > .v-col { padding-inline: 0; }
  .fleet-hero__actions { padding-top: 14px; }
  .fleet-hero__actions .v-btn { padding-inline: 7px; font-size: 0.78rem; }
  .fleet-monitor { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .fleet-traffic__head { align-items: flex-start; flex-direction: column; gap: 3px; }
  .fleet-traffic__head strong { max-width: 100%; text-align: left; }
  .fleet-detail__grid, .fleet-config-snapshot { grid-template-columns: 1fr; }
  .fleet-drift-item { grid-template-columns: 1fr auto; }
  .fleet-drift-item span, .fleet-drift-item strong { grid-column: 1 / -1; }
  .fleet-card__actions .v-btn { flex: 1 1 auto; }
}

.fleet-dialog__title { display: flex; align-items: center; justify-content: space-between; }
.fleet-config-row { display: grid; grid-template-columns: 0.8fr 1.5fr 1.3fr auto auto; align-items: center; gap: 10px; padding: 10px 0; }
.fleet-dialog__add { margin-top: 8px; }
.fleet-remote-inbound__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; padding: 22px 24px 16px; border-bottom: 1px solid var(--np-border); }
.fleet-remote-inbound__header > div { min-width: 0; }
.fleet-remote-inbound__header h2 { margin: 3px 0 4px; font-size: 1.35rem; letter-spacing: -.025em; }
.fleet-remote-inbound__header p { max-width: 650px; margin: 0; color: var(--np-text-muted); font-size: .82rem; line-height: 1.55; }
.fleet-remote-inbound__eyebrow { color: rgb(var(--v-theme-primary)); font-size: .67rem; font-weight: 800; letter-spacing: .16em; text-transform: uppercase; }
.fleet-remote-inbound__section-head { display: flex; align-items: center; gap: 12px; }
.fleet-remote-inbound__section-head h3 { margin: 0 0 3px; font-size: 1rem; }
.fleet-remote-inbound__section-head p { margin: 0; color: var(--np-text-muted); font-size: .78rem; line-height: 1.5; }
.fleet-remote-inbound__section-icon { display: grid; flex: 0 0 42px; width: 42px; height: 42px; place-items: center; border-radius: 14px; background: rgba(10, 132, 255, .11); color: rgb(var(--v-theme-primary)); }
.fleet-target-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px; }
.fleet-target-grid button { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 13px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface); color: var(--np-text); text-align: left; }
.fleet-target-grid button.is-selected { border-color: rgba(10, 132, 255, .52); background: rgba(10, 132, 255, .08); }
.fleet-target-grid__check { display: grid; width: 30px; height: 30px; place-items: center; border-radius: 10px; background: var(--np-surface-muted); color: var(--np-text-muted); }
.fleet-target-grid button.is-selected .fleet-target-grid__check { background: rgb(var(--v-theme-primary)); color: white; }
.fleet-target-grid button > span:nth-child(2) { display: grid; gap: 3px; min-width: 0; }
.fleet-target-grid button strong, .fleet-target-grid button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-target-grid button strong { font-size: .84rem; }
.fleet-target-grid button small, .fleet-target-grid button em { color: var(--np-text-muted); font-size: .68rem; font-style: normal; }
.fleet-remote-inbound__results { display: grid; gap: 8px; }
.fleet-remote-inbound__result { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border: 1px solid var(--np-border); border-radius: 14px; }
.fleet-remote-inbound__result > div { display: grid; gap: 3px; min-width: 0; }
.fleet-remote-inbound__result span { color: var(--np-text-muted); font-size: .78rem; overflow-wrap: anywhere; }
.fleet-remote-inbound__result small { color: var(--np-text-muted); font-size: .7rem; overflow-wrap: anywhere; }
.fleet-remote-inbound__result.is-success { color: rgb(var(--v-theme-success)); background: rgba(34, 197, 94, .06); }
.fleet-remote-inbound__result.is-error { color: rgb(var(--v-theme-error)); background: rgba(239, 68, 68, .06); }
.fleet-remote-inbound__footer { min-height: 64px; padding: 10px 20px; border-top: 1px solid var(--np-border); background: var(--np-surface-muted); }
.fleet-remote-inbound { overflow: hidden; }
.fleet-remote-inbound__body { display: grid; gap: 16px; max-height: min(72vh, 820px); padding: 18px 24px; }
.fleet-remote-inbound__section { display: grid; gap: 12px; }
.fleet-remote-inbound__targets { display: grid; gap: 9px; }
.fleet-remote-target { display: grid; gap: 10px; padding: 13px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface); }
.fleet-remote-target > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.fleet-remote-target > header > div { display: grid; gap: 3px; min-width: 0; }
.fleet-remote-target > header strong { font-size: .84rem; }
.fleet-remote-target > header small { color: var(--np-text-muted); font-size: .7rem; overflow-wrap: anywhere; }
.fleet-remote-target__settings { display: grid; grid-template-columns: 1fr 1fr; align-items: start; gap: 10px; }
.fleet-remote-target__certificate { grid-column: 1 / -1; color: var(--np-text-muted); font-size: .68rem; overflow-wrap: anywhere; }
.fleet-remote-target__hint, .fleet-remote-target__error { margin: 0; color: var(--np-text-muted); font-size: .72rem; overflow-wrap: anywhere; }
.fleet-remote-target__error { color: rgb(var(--v-theme-error)); }
.fleet-remote-target > footer { padding-top: 8px; border-top: 1px solid var(--np-border); color: var(--np-text-muted); font-size: .7rem; overflow-wrap: anywhere; }
.fleet-remote-target > footer strong { color: var(--np-text); }

@media (max-width: 800px) {
  .fleet-hero { padding: 18px; }
  .fleet-hero__actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
  .fleet-hero__actions .v-btn { width: 100%; min-width: 0; }
  .fleet-hero__meta { flex-wrap: wrap; }
  .fleet-config-row { grid-template-columns: 1fr; padding: 14px 0; border-bottom: 1px solid var(--np-border); }
  .fleet-remote-inbound__header { padding: 18px 16px 14px; }
  .fleet-remote-inbound__body { padding: 16px 14px; }
  .fleet-remote-target__settings { grid-template-columns: 1fr; }
  .fleet-remote-target__certificate { grid-column: 1; }
  .fleet-target-grid { grid-template-columns: 1fr; }
  .fleet-remote-inbound__footer { flex-wrap: wrap; padding: 9px 12px; }
  .fleet-remote-inbound__footer .v-btn { flex: 1 1 auto; }
}

@media (max-width: 1279px) {
  .fleet-summary { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (max-width: 599px) {
  .fleet-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .fleet-summary__col--last { grid-column: 1 / -1; }
}
</style>
