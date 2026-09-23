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
            <v-btn variant="outlined" color="secondary" :disabled="loading" @click="openOrchestration">
              <v-icon icon="mdi-layers-triple-outline" start />
              {{ $t('ui.fleet.orchestration') }}
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

      <v-row class="fleet-summary" dense>
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

      <v-row class="fleet-grid" dense>
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

    <v-dialog v-model="showOrchestration" max-width="840" scrollable>
      <v-card rounded="xl" class="fleet-dialog fleet-rollout">
        <header class="fleet-rollout__header">
          <div>
            <span class="fleet-rollout__eyebrow">{{ $t('ui.fleet.safeRollout') }}</span>
            <h2>{{ $t('ui.fleet.orchestrationTitle') }}</h2>
            <p>{{ $t('ui.fleet.orchestrationHint') }}</p>
          </div>
          <v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="showOrchestration = false" />
        </header>

        <nav class="fleet-rollout__steps" :aria-label="$t('ui.fleet.rolloutProgress')">
          <button v-for="step in 3" :key="step" type="button" :class="{ 'is-active': orchestrationStep === step, 'is-done': orchestrationStep > step }" :disabled="step > orchestrationStep" @click="orchestrationStep = step">
            <span>{{ orchestrationStep > step ? '✓' : step }}</span>
            <strong>{{ $t(`ui.fleet.rolloutStep${step}`) }}</strong>
          </button>
        </nav>

        <v-card-text class="fleet-rollout__body">
          <v-window v-model="orchestrationStep" :touch="false">
            <v-window-item :value="1">
              <section class="fleet-rollout__panel">
                <div class="fleet-rollout__section-head">
                  <div class="fleet-rollout__section-icon"><v-icon icon="mdi-layers-outline" /></div>
                  <div><h3>{{ $t('ui.fleet.chooseTemplate') }}</h3><p>{{ $t('ui.fleet.chooseTemplateHint') }}</p></div>
                </div>

                <div v-if="templates.length" class="fleet-template-list">
                  <button v-for="item in templates" :key="item.id" type="button" class="fleet-template-card" :class="{ 'is-selected': selectedTemplateId === item.id }" @click="selectTemplate(item.id)">
                    <span class="fleet-template-card__icon"><v-icon icon="mdi-file-document-check-outline" /></span>
                    <span class="fleet-template-card__copy"><strong>{{ item.name }}</strong><small>{{ templateSectionLabels(item).join(' · ') }}</small></span>
                    <v-icon :icon="selectedTemplateId === item.id ? 'mdi-check-circle' : 'mdi-chevron-right'" />
                  </button>
                  <div v-if="selectedTemplateId" class="fleet-template-list__actions">
                    <v-btn size="small" color="error" variant="text" @click="deleteTemplate"><v-icon icon="mdi-delete-outline" start />{{ $t('actions.del') }}</v-btn>
                    <v-btn color="primary" @click="goToTargets"><v-icon icon="mdi-arrow-right" end />{{ $t('ui.fleet.useTemplate') }}</v-btn>
                  </div>
                </div>
                <div v-else class="fleet-rollout__empty"><v-icon icon="mdi-file-plus-outline" /><span>{{ $t('ui.fleet.noTemplates') }}</span></div>

                <div class="fleet-rollout__divider"><span>{{ $t('ui.fleet.orCreateTemplate') }}</span></div>

                <div class="fleet-template-create">
                  <v-text-field v-model="templateName" :label="$t('ui.fleet.templateName')" :placeholder="$t('ui.fleet.templateNameExample')" variant="outlined" density="comfortable" hide-details />
                  <div class="fleet-template-scope">
                    <button v-for="option in templateSectionOptions" :key="option.key" type="button" :aria-pressed="templateSections[option.key]" :class="{ 'is-selected': templateSections[option.key] }" @click="toggleTemplateSection(option.key)">
                      <v-icon :icon="option.icon" />
                      <span><strong>{{ option.label }}</strong><small>{{ option.hint }}</small></span>
                      <v-icon :icon="templateSections[option.key] ? 'mdi-check-circle' : 'mdi-circle-outline'" />
                    </button>
                  </div>
                  <v-btn color="primary" size="large" :loading="orchestrationLoading === 'capture'" :disabled="!canCaptureTemplate" @click="captureTemplate">
                    <v-icon icon="mdi-content-save-plus-outline" start />{{ $t('ui.fleet.saveAndContinue') }}
                  </v-btn>
                </div>
              </section>
            </v-window-item>

            <v-window-item :value="2">
              <section class="fleet-rollout__panel">
                <div class="fleet-rollout__section-head">
                  <div class="fleet-rollout__section-icon"><v-icon icon="mdi-server-network-outline" /></div>
                  <div><h3>{{ $t('ui.fleet.chooseTargets') }}</h3><p>{{ $t('ui.fleet.chooseTargetsHint') }}</p></div>
                </div>
                <div class="fleet-rollout__selection-summary"><span>{{ $t('ui.fleet.selectedTemplate') }}</span><strong>{{ selectedTemplate?.name }}</strong></div>
                <div class="fleet-target-grid">
                  <button v-for="server in deployableServers" :key="server.id" type="button" :aria-pressed="selectedTargets.includes(server.id)" :class="{ 'is-selected': selectedTargets.includes(server.id) }" @click="toggleTarget(server.id)">
                    <span class="fleet-target-grid__check"><v-icon :icon="selectedTargets.includes(server.id) ? 'mdi-check' : 'mdi-plus'" /></span>
                    <span><strong>{{ server.name }}</strong><small>{{ server.id === 'local' ? $t('ui.common.local') : server.url }}</small></span>
                    <em>{{ server.latencyMs ? `${server.latencyMs} ms` : $t('ui.common.online') }}</em>
                  </button>
                </div>
                <v-alert v-if="!selectedTargets.length" type="warning" variant="tonal" density="compact">{{ $t('ui.fleet.selectTargetsFirst') }}</v-alert>
                <div v-else class="fleet-canary-box">
                  <div><strong>{{ $t('ui.fleet.canaryServer') }}</strong><span>{{ $t('ui.fleet.canaryHint') }}</span></div>
                  <v-select v-model="canaryTarget" :items="selectedTargetItems" :placeholder="$t('ui.fleet.noCanary')" clearable variant="outlined" density="comfortable" hide-details />
                </div>
                <div class="fleet-rollout__order"><v-icon icon="mdi-sort-ascending" /><span>{{ rolloutOrderLabel }}</span></div>
              </section>
            </v-window-item>

            <v-window-item :value="3">
              <section class="fleet-rollout__panel">
                <div class="fleet-rollout__section-head">
                  <div class="fleet-rollout__section-icon"><v-icon :icon="deploymentComplete ? 'mdi-check-decagram-outline' : 'mdi-file-search-outline'" /></div>
                  <div><h3>{{ deploymentComplete ? $t('ui.fleet.rolloutComplete') : $t('ui.fleet.reviewChanges') }}</h3><p>{{ deploymentComplete ? $t('ui.fleet.rolloutCompleteHint') : $t('ui.fleet.reviewChangesHint') }}</p></div>
                </div>
                <div class="fleet-rollout__review-meta">
                  <div><span>{{ $t('ui.fleet.selectedTemplate') }}</span><strong>{{ selectedTemplate?.name }}</strong></div>
                  <div><span>{{ $t('ui.fleet.targetServers') }}</span><strong>{{ selectedTargetNames.join('、') }}</strong></div>
                </div>
                <div class="fleet-orchestration__results">
                  <div v-for="result in templateResults" :key="result.id" class="fleet-orchestration__result" :class="result.success ? 'is-success' : 'is-error'">
                    <div><strong>{{ result.name }}</strong><span v-if="result.preview">{{ previewSummary(result.preview) }}</span><span v-else-if="result.error">{{ result.error }}</span><span v-else>{{ $t('ui.fleet.deployed') }}</span></div>
                    <v-icon :icon="result.success ? 'mdi-check-circle' : 'mdi-alert-circle'" />
                  </div>
                </div>
                <v-alert v-if="!deploymentComplete" :type="canDeployTemplate ? 'success' : 'error'" variant="tonal" density="compact">
                  {{ canDeployTemplate ? $t('ui.fleet.previewPassed') : $t('ui.fleet.previewFailed') }}
                </v-alert>
              </section>
            </v-window-item>
          </v-window>
        </v-card-text>

        <v-card-actions class="fleet-rollout__footer">
          <v-btn v-if="orchestrationStep > 1 && !deploymentComplete" variant="text" @click="orchestrationStep -= 1"><v-icon icon="mdi-arrow-left" start />{{ $t('ui.fleet.previousStep') }}</v-btn>
          <v-spacer />
          <v-btn variant="text" @click="showOrchestration = false">{{ deploymentComplete ? $t('ui.common.close') : $t('ui.common.cancel') }}</v-btn>
          <v-btn v-if="orchestrationStep === 2" color="primary" :loading="orchestrationLoading === 'preview'" :disabled="!canRunTemplate" @click="previewTemplate"><v-icon icon="mdi-file-compare" start />{{ $t('ui.fleet.previewChanges') }}</v-btn>
          <v-btn v-if="orchestrationStep === 3 && !deploymentComplete" color="primary" :loading="orchestrationLoading === 'deploy'" :disabled="!canDeployTemplate" @click="deployTemplate"><v-icon icon="mdi-rocket-launch-outline" start />{{ $t('ui.fleet.confirmRollout') }}</v-btn>
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
                <small>基线 {{ formatDriftValue(item.expected) }}</small>
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
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'
import FleetTrafficHistory from '@/components/FleetTrafficHistory.vue'
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

type FleetTemplateSectionKey = 'tls' | 'inbounds' | 'clients' | 'route' | 'dns'

type FleetTemplate = {
  id: string
  name: string
  createdAt: string
  sections: Record<FleetTemplateSectionKey, boolean>
}

type FleetTemplatePreview = {
  tlsAdd: number
  tlsUpdate: number
  inboundAdd: number
  inboundUpdate: number
  clientAdd: number
  clientUpdate: number
  configUpdates?: string[]
}

type FleetTemplateResult = {
  id: string
  name: string
  success: boolean
  preview?: FleetTemplatePreview
  error?: string
}

const t = i18n.global.t

const loading = ref(true)
const saving = ref(false)
const showConfig = ref(false)
const showDetails = ref(false)
const showOrchestration = ref(false)
const orchestrationStep = ref(1)
const deploymentComplete = ref(false)
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
const templates = ref<FleetTemplate[]>([])
const templateName = ref('')
const templateSections = ref<Record<FleetTemplateSectionKey, boolean>>({ tls: false, inbounds: true, clients: true, route: true, dns: true })
const selectedTemplateId = ref('')
const selectedTargets = ref<string[]>([])
const canaryTarget = ref<string | null>(null)
const templateResults = ref<FleetTemplateResult[]>([])
const orchestrationLoading = ref<'' | 'capture' | 'preview' | 'deploy'>('')
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
const deployableServers = computed(() => servers.value.filter(server => server.reachable && server.enabled))
const selectedTemplate = computed(() => templates.value.find(item => item.id === selectedTemplateId.value))
const selectedTargetItems = computed(() => deployableServers.value
  .filter(server => selectedTargets.value.includes(server.id))
  .map(server => ({ title: server.name, value: server.id })))
const selectedTargetNames = computed(() => deployableServers.value.filter(server => selectedTargets.value.includes(server.id)).map(server => server.name))
const templateSectionOptions = computed<Array<{ key: FleetTemplateSectionKey; label: string; hint: string; icon: string }>>(() => [
  { key: 'inbounds', label: t('pages.inbounds'), hint: t('ui.fleet.scopeInbounds'), icon: 'mdi-download-network-outline' },
  { key: 'clients', label: t('pages.clients'), hint: t('ui.fleet.scopeClients'), icon: 'mdi-account-multiple-outline' },
  { key: 'tls', label: t('pages.tls'), hint: t('ui.fleet.scopeTls'), icon: 'mdi-certificate-outline' },
  { key: 'route', label: t('pages.rules'), hint: t('ui.fleet.scopeRoute'), icon: 'mdi-routes' },
  { key: 'dns', label: 'DNS', hint: t('ui.fleet.scopeDns'), icon: 'mdi-dns-outline' },
])
const canCaptureTemplate = computed(() => Boolean(templateName.value.trim() && Object.values(templateSections.value).some(Boolean)))
const canRunTemplate = computed(() => Boolean(selectedTemplateId.value && selectedTargets.value.length))
const canDeployTemplate = computed(() => canRunTemplate.value && templateResults.value.length > 0 && templateResults.value.every(result => result.success))
const rolloutOrderLabel = computed(() => {
  if (!selectedTargets.value.length) return t('ui.fleet.noTargetsSelected')
  const names = [...selectedTargetNames.value]
  if (canaryTarget.value) {
    const canary = deployableServers.value.find(server => server.id === canaryTarget.value)?.name
    const currentIndex = canary ? names.indexOf(canary) : -1
    if (canary && currentIndex >= 0) {
      names.splice(currentIndex, 1)
      names.unshift(t('ui.fleet.rolloutMarker', { name: canary, role: t('ui.fleet.canary') }))
    }
  }
  const localIndex = selectedTargets.value.indexOf('local')
  if (localIndex >= 0) {
    const localName = deployableServers.value.find(server => server.id === 'local')?.name
    if (localName) {
      const currentIndex = names.indexOf(localName)
      if (currentIndex >= 0) names.splice(currentIndex, 1)
      names.push(t('ui.fleet.rolloutMarker', { name: localName, role: t('ui.fleet.last') }))
    }
  }
  return t('ui.fleet.rolloutOrder', { targets: names.join(' → ') })
})
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

const loadTemplates = async () => {
  const response = await HttpUtils.get('api/fleetTemplates')
  if (response.success) {
    templates.value = Array.isArray(response.obj) ? response.obj : []
    if (selectedTemplateId.value && !templates.value.some(item => item.id === selectedTemplateId.value)) selectedTemplateId.value = ''
  }
}

const openOrchestration = async () => {
  orchestrationStep.value = 1
  deploymentComplete.value = false
  templateName.value = ''
  templateSections.value = { tls: false, inbounds: true, clients: true, route: true, dns: true }
  templateResults.value = []
  selectedTargets.value = []
  canaryTarget.value = null
  selectedTemplateId.value = ''
  await loadTemplates()
  showOrchestration.value = true
}

const selectTemplate = (id: string) => {
  selectedTemplateId.value = id
  selectedTargets.value = []
  canaryTarget.value = null
  templateResults.value = []
  deploymentComplete.value = false
}

const templateSectionLabels = (template: FleetTemplate) => templateSectionOptions.value
  .filter(option => template.sections?.[option.key])
  .map(option => option.label)

const toggleTemplateSection = (key: FleetTemplateSectionKey) => {
  templateSections.value[key] = !templateSections.value[key]
}

const goToTargets = () => {
  if (!selectedTemplateId.value) return
  orchestrationStep.value = 2
}

const toggleTarget = (id: string) => {
  if (selectedTargets.value.includes(id)) {
    selectedTargets.value = selectedTargets.value.filter(target => target !== id)
    if (canaryTarget.value === id) canaryTarget.value = null
  } else {
    selectedTargets.value = [...selectedTargets.value, id]
  }
  templateResults.value = []
  deploymentComplete.value = false
}

const captureTemplate = async () => {
  orchestrationLoading.value = 'capture'
  const response = await HttpUtils.post('api/fleetTemplateCapture', {
    name: templateName.value,
    sections: JSON.stringify(templateSections.value),
  })
  if (response.success) {
    templateName.value = ''
    await loadTemplates()
    selectedTemplateId.value = response.obj?.id ?? ''
    orchestrationStep.value = 2
  }
  orchestrationLoading.value = ''
}

const deleteTemplate = async () => {
  if (!selectedTemplateId.value || !window.confirm(t('ui.fleet.confirmDeleteTemplate'))) return
  const response = await HttpUtils.post('api/fleetTemplateDelete', { id: selectedTemplateId.value })
  if (response.success) {
    selectedTemplateId.value = ''
    templateResults.value = []
    orchestrationStep.value = 1
    await loadTemplates()
  }
}

const previewTemplate = async () => {
  orchestrationLoading.value = 'preview'
  templateResults.value = []
  const response = await HttpUtils.post('api/fleetTemplatePreview', {
    id: selectedTemplateId.value,
    targets: JSON.stringify(selectedTargets.value),
  })
  if (response.success) {
    templateResults.value = response.obj ?? []
    orchestrationStep.value = 3
    deploymentComplete.value = false
  }
  orchestrationLoading.value = ''
}

const deployTemplate = async () => {
  if (!window.confirm(t('ui.fleet.confirmDeployTemplate'))) return
  orchestrationLoading.value = 'deploy'
  const response = await HttpUtils.post('api/fleetTemplateDeploy', {
    id: selectedTemplateId.value,
    targets: JSON.stringify(selectedTargets.value),
    canary: canaryTarget.value ?? '',
  })
  if (response.success) {
    templateResults.value = response.obj ?? []
    deploymentComplete.value = templateResults.value.length > 0 && templateResults.value.every(result => result.success)
    await loadFleet(true)
  }
  orchestrationLoading.value = ''
}

const previewSummary = (preview: FleetTemplatePreview) => t('ui.fleet.previewSummary', {
  inboundAdd: preview.inboundAdd ?? 0,
  inboundUpdate: preview.inboundUpdate ?? 0,
  clientAdd: preview.clientAdd ?? 0,
  clientUpdate: preview.clientUpdate ?? 0,
  tls: (preview.tlsAdd ?? 0) + (preview.tlsUpdate ?? 0),
  config: preview.configUpdates?.join(' / ') || '-',
})

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
.fleet-rollout { overflow: hidden; }
.fleet-rollout__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; padding: 22px 24px 16px; border-bottom: 1px solid var(--np-border); }
.fleet-rollout__header > div { min-width: 0; }
.fleet-rollout__header h2 { margin: 3px 0 4px; font-size: 1.35rem; letter-spacing: -.025em; }
.fleet-rollout__header p { max-width: 650px; margin: 0; color: var(--np-text-muted); font-size: .82rem; line-height: 1.55; }
.fleet-rollout__eyebrow { color: rgb(var(--v-theme-primary)); font-size: .67rem; font-weight: 800; letter-spacing: .16em; text-transform: uppercase; }
.fleet-rollout__steps { display: grid; grid-template-columns: repeat(3, 1fr); padding: 12px 24px; border-bottom: 1px solid var(--np-border); background: var(--np-surface-muted); }
.fleet-rollout__steps button { position: relative; display: flex; align-items: center; justify-content: center; gap: 8px; min-width: 0; padding: 8px 10px; color: var(--np-text-muted); }
.fleet-rollout__steps button:not(:last-child)::after { position: absolute; right: -10%; width: 20%; height: 1px; background: var(--np-border); content: ''; }
.fleet-rollout__steps button > span { display: grid; flex: 0 0 26px; width: 26px; height: 26px; place-items: center; border: 1px solid var(--np-border); border-radius: 50%; background: var(--np-surface); font-size: .72rem; font-weight: 800; }
.fleet-rollout__steps button strong { overflow: hidden; font-size: .78rem; text-overflow: ellipsis; white-space: nowrap; }
.fleet-rollout__steps button.is-active { color: rgb(var(--v-theme-primary)); }
.fleet-rollout__steps button.is-active > span { border-color: rgb(var(--v-theme-primary)); background: rgb(var(--v-theme-primary)); color: white; box-shadow: 0 5px 14px rgba(37, 99, 235, .22); }
.fleet-rollout__steps button.is-done { color: rgb(var(--v-theme-success)); }
.fleet-rollout__steps button.is-done > span { border-color: rgba(34, 197, 94, .35); background: rgba(34, 197, 94, .12); }
.fleet-rollout__body { padding: 22px 24px 18px; }
.fleet-rollout__panel { display: grid; gap: 18px; min-height: 350px; }
.fleet-rollout__section-head { display: flex; align-items: center; gap: 12px; }
.fleet-rollout__section-head h3 { margin: 0 0 3px; font-size: 1rem; }
.fleet-rollout__section-head p { margin: 0; color: var(--np-text-muted); font-size: .78rem; line-height: 1.5; }
.fleet-rollout__section-icon { display: grid; flex: 0 0 42px; width: 42px; height: 42px; place-items: center; border-radius: 14px; background: rgba(10, 132, 255, .11); color: rgb(var(--v-theme-primary)); }
.fleet-template-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.fleet-template-card { display: flex; align-items: center; gap: 11px; min-width: 0; padding: 13px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface); color: var(--np-text); text-align: left; transition: border-color .18s ease, background .18s ease, transform .18s ease; }
.fleet-template-card:hover { border-color: rgba(10, 132, 255, .34); transform: translateY(-1px); }
.fleet-template-card.is-selected { border-color: rgba(10, 132, 255, .52); background: rgba(10, 132, 255, .08); }
.fleet-template-card__icon { display: grid; flex: 0 0 38px; width: 38px; height: 38px; place-items: center; border-radius: 12px; background: var(--np-surface-muted); color: rgb(var(--v-theme-primary)); }
.fleet-template-card__copy { display: grid; flex: 1; gap: 3px; min-width: 0; }
.fleet-template-card__copy strong, .fleet-template-card__copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-template-card__copy strong { font-size: .86rem; }
.fleet-template-card__copy small { color: var(--np-text-muted); font-size: .7rem; }
.fleet-template-list__actions { display: flex; grid-column: 1 / -1; align-items: center; justify-content: flex-end; gap: 8px; padding-top: 2px; }
.fleet-rollout__empty { display: flex; align-items: center; justify-content: center; gap: 10px; min-height: 82px; border: 1px dashed var(--np-border); border-radius: 15px; color: var(--np-text-muted); font-size: .8rem; }
.fleet-rollout__divider { display: flex; align-items: center; gap: 12px; color: var(--np-text-muted); font-size: .72rem; }
.fleet-rollout__divider::before, .fleet-rollout__divider::after { flex: 1; height: 1px; background: var(--np-border); content: ''; }
.fleet-template-create { display: grid; gap: 13px; padding: 16px; border: 1px solid var(--np-border); border-radius: 18px; background: var(--np-surface-muted); }
.fleet-template-scope { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.fleet-template-scope button { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 11px 12px; border: 1px solid var(--np-border); border-radius: 13px; background: var(--np-surface); color: var(--np-text); text-align: left; }
.fleet-template-scope button.is-selected { border-color: rgba(10, 132, 255, .42); background: rgba(10, 132, 255, .07); color: rgb(var(--v-theme-primary)); }
.fleet-template-scope button > span { display: grid; gap: 2px; min-width: 0; }
.fleet-template-scope button strong { color: var(--np-text); font-size: .8rem; }
.fleet-template-scope button small { overflow: hidden; color: var(--np-text-muted); font-size: .68rem; text-overflow: ellipsis; white-space: nowrap; }
.fleet-rollout__selection-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 11px 14px; border-radius: 13px; background: rgba(10, 132, 255, .08); }
.fleet-rollout__selection-summary span { color: var(--np-text-muted); font-size: .72rem; }
.fleet-target-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 9px; }
.fleet-target-grid button { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 13px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface); color: var(--np-text); text-align: left; }
.fleet-target-grid button.is-selected { border-color: rgba(10, 132, 255, .52); background: rgba(10, 132, 255, .08); }
.fleet-target-grid__check { display: grid; width: 30px; height: 30px; place-items: center; border-radius: 10px; background: var(--np-surface-muted); color: var(--np-text-muted); }
.fleet-target-grid button.is-selected .fleet-target-grid__check { background: rgb(var(--v-theme-primary)); color: white; }
.fleet-target-grid button > span:nth-child(2) { display: grid; gap: 3px; min-width: 0; }
.fleet-target-grid button strong, .fleet-target-grid button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fleet-target-grid button strong { font-size: .84rem; }
.fleet-target-grid button small, .fleet-target-grid button em { color: var(--np-text-muted); font-size: .68rem; font-style: normal; }
.fleet-canary-box { display: grid; grid-template-columns: minmax(0, .8fr) minmax(260px, 1.2fr); align-items: center; gap: 14px; padding: 14px; border: 1px solid var(--np-border); border-radius: 15px; background: var(--np-surface-muted); }
.fleet-canary-box > div { display: grid; gap: 3px; }
.fleet-canary-box span { color: var(--np-text-muted); font-size: .7rem; line-height: 1.45; }
.fleet-rollout__order { display: flex; align-items: flex-start; gap: 9px; padding: 11px 13px; border-radius: 13px; background: rgba(14, 165, 233, .09); color: rgb(var(--v-theme-info)); font-size: .76rem; line-height: 1.5; }
.fleet-rollout__review-meta { display: grid; grid-template-columns: .7fr 1.3fr; gap: 9px; }
.fleet-rollout__review-meta > div { display: grid; gap: 4px; padding: 11px 13px; border: 1px solid var(--np-border); border-radius: 13px; }
.fleet-rollout__review-meta span { color: var(--np-text-muted); font-size: .7rem; }
.fleet-rollout__review-meta strong { overflow-wrap: anywhere; font-size: .82rem; }
.fleet-orchestration__results { display: grid; gap: 8px; }
.fleet-orchestration__result { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border: 1px solid var(--np-border); border-radius: 14px; }
.fleet-orchestration__result > div { display: grid; gap: 3px; min-width: 0; }
.fleet-orchestration__result span { color: var(--np-text-muted); font-size: .78rem; overflow-wrap: anywhere; }
.fleet-orchestration__result.is-success { color: rgb(var(--v-theme-success)); background: rgba(34, 197, 94, .06); }
.fleet-orchestration__result.is-error { color: rgb(var(--v-theme-error)); background: rgba(239, 68, 68, .06); }
.fleet-rollout__footer { min-height: 64px; padding: 10px 20px; border-top: 1px solid var(--np-border); background: var(--np-surface-muted); }

@media (max-width: 800px) {
  .fleet-hero { padding: 18px; }
  .fleet-hero__actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
  .fleet-hero__actions .v-btn { width: 100%; min-width: 0; }
  .fleet-hero__meta { flex-wrap: wrap; }
  .fleet-config-row { grid-template-columns: 1fr; padding: 14px 0; border-bottom: 1px solid var(--np-border); }
  .fleet-rollout__header { padding: 18px 16px 14px; }
  .fleet-rollout__steps { padding-inline: 8px; }
  .fleet-rollout__steps button { padding-inline: 4px; }
  .fleet-rollout__steps button strong { font-size: .7rem; }
  .fleet-rollout__body { padding: 18px 14px 14px; }
  .fleet-template-list, .fleet-template-scope, .fleet-target-grid, .fleet-rollout__review-meta { grid-template-columns: 1fr; }
  .fleet-canary-box { grid-template-columns: 1fr; }
  .fleet-template-list__actions { position: sticky; bottom: 0; padding: 8px; border-radius: 12px; background: var(--np-surface); box-shadow: 0 -8px 24px rgba(15, 23, 42, .08); }
  .fleet-rollout__footer { flex-wrap: wrap; padding: 9px 12px; }
  .fleet-rollout__footer .v-btn { flex: 1 1 auto; }
}

@media (max-width: 1279px) {
  .fleet-summary { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (max-width: 599px) {
  .fleet-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .fleet-summary__col--last { grid-column: 1 / -1; }
}
</style>
