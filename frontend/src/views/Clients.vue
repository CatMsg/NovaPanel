
<template>
  <ClientModal 
    v-model="modal.visible"
    :visible="modal.visible"
    :id="modal.id"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeModal"
  />
  <ClientAddBulk 
    v-model="addBulkModal"
    :visible="addBulkModal"
    :groups="groups"
    :inboundTags="inboundTags"
    @close="closeAddBulk"
  />
  <ClientEditBulk 
    v-model="editBulkModal"
    :visible="editBulkModal"
    :inboundTags="inboundTags"
    :clients="clients"
    @close="closeEditBulk"
  />
  <QrCode
    v-model="qrcode.visible"
    :visible="qrcode.visible"
    :id="qrcode.id"
    @close="closeQrCode"
  />
  <Stats
    v-model="stats.visible"
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="closeStats"
  />
  <ClientHistory
    :visible="historyModal.visible"
    :id="historyModal.id"
    @close="closeHistory"
  />
  <v-dialog v-model="deleteDialog" max-width="420">
    <v-card rounded="xl">
      <v-card-title>{{ $t('actions.del') }}</v-card-title>
      <v-divider />
      <v-card-text>
        {{ $t('confirm') }}
        <strong v-if="deleteTarget">“{{ deleteTarget.name }}”</strong>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="deleteDialog = false">{{ $t('no') }}</v-btn>
        <v-btn color="error" variant="tonal" :loading="deleteLoading" @click="confirmDelete">{{ $t('yes') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
  <v-card class="clients-hero" rounded="xl" variant="flat">
    <div class="clients-hero__topline">
      <span class="clients-hero__badge">{{ $t('pages.clients') }}</span>
      <span class="clients-hero__badge clients-hero__badge--soft">
        {{ activeFilterCount > 0 ? `${activeFilterCount} ${$t('client.filters')}` : $t('main.hero.live') }}
      </span>
    </div>

    <v-row class="clients-hero__content" align="center">
      <v-col cols="12" lg="7">
        <div class="clients-hero__title-row">
          <div class="clients-hero__icon">
            <v-icon icon="mdi-account-group-outline" size="32" />
          </div>
          <div>
            <h1 class="clients-hero__title">{{ $t('pages.clients') }}</h1>
            <p class="clients-hero__subtitle">
              {{ $t('client.manageDesc') }}
            </p>
          </div>
        </div>
        <div class="clients-hero__meta">
          <span>{{ $t('client.total') }} {{ clients.length }}</span>
          <span>•</span>
          <span>{{ $t('online') }} {{ onlineCount }}</span>
          <template v-if="activeFilterCount > 0">
            <span>•</span>
            <span>{{ $t('client.showing') }} {{ visibleClients.length }}</span>
          </template>
        </div>
      </v-col>
      <v-col cols="12" lg="5" class="clients-hero__actions">
        <v-btn color="primary" size="large" class="clients-hero__primary" @click="showModal(0)">
          <v-icon icon="mdi-plus" start />
          {{ $t('actions.add') }}
        </v-btn>
        <v-menu v-model="actionMenu" :close-on-content-click="false" location="bottom center">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" class="clients-hero__icon-btn" variant="flat" icon :aria-label="$t('client.bulkActions')" :title="$t('client.bulkActions')">
              <v-icon icon="mdi-tools" />
            </v-btn>
          </template>
          <v-list density="compact" nav>
            <v-list-item link @click="addBulk">
              <template v-slot:prepend>
                <v-icon icon="mdi-account-multiple-plus"></v-icon>
              </template>
              <v-list-item-title v-text="$t('actions.addbulk')"></v-list-item-title>
            </v-list-item>
            <v-list-item link @click="editBulk">
              <template v-slot:prepend>
                <v-icon icon="mdi-account-multiple-check"></v-icon>
              </template>
              <v-list-item-title v-text="$t('actions.editbulk')"></v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
        <v-menu v-model="filterMenu" :close-on-content-click="false" location="bottom center">
          <template v-slot:activator="{ props }">
            <v-btn v-bind="props" class="clients-hero__icon-btn" variant="flat" icon :aria-label="$t('client.filters')" :title="$t('client.filters')">
              <v-icon
                :icon="activeFilterCount > 0 ? 'mdi-filter-check-outline' : 'mdi-filter-menu-outline'"
                :color="activeFilterCount > 0 ? 'primary' : ''"
              />
            </v-btn>
          </template>
          <v-card class="clients-filter" rounded="xl">
            <v-container class="clients-filter__container">
              <v-row>
                <v-col>
                  <v-select
                    variant="underlined"
                    density="compact"
                    :label="$t('type')"
                    :items="filterItems"
                    v-model="filterSettings.state"
                  />
                </v-col>
              </v-row>
              <v-row>
                <v-col>
                  <v-select
                    variant="underlined"
                    density="compact"
                    :label="$t('client.group')"
                    :items="[ {title: $t('all'), value: '-'}, ...groups.map(g => ({ title: g.length>0 ? g : $t('none'), value: g}))]"
                    v-model="filterSettings.group"
                  />
                </v-col>
              </v-row>
            </v-container>
            <v-card-actions class="clients-filter__actions">
              <v-spacer />
              <v-btn color="blue-darken-1" variant="outlined" @click="clearFilter">
                {{ $t('reset') }}
              </v-btn>
              <v-btn color="blue-darken-1" variant="tonal" @click="doFilter">
                {{ $t('actions.close') }}
              </v-btn>
            </v-card-actions>
          </v-card>
        </v-menu>
      </v-col>
    </v-row>
  </v-card>

  <v-card class="clients-table-card" rounded="xl" variant="flat">
    <div class="clients-table-card__head">
      <div>
        <div class="clients-table-card__title">{{ $t('pages.clients') }}</div>
        <div class="clients-table-card__subtitle">{{ $t('client.manageHint') }}</div>
        <div v-if="activeFilterCount > 0" class="clients-table-card__filters">
          <v-chip v-if="quickSearch" size="small" closable @click:close="quickSearch = ''">
            {{ $t('search') }}: {{ quickSearch }}
          </v-chip>
          <v-chip v-if="filterSettings.group !== '-'" size="small" closable @click:close="filterSettings.group = '-'">
            {{ $t('client.group') }}: {{ filterSettings.group || $t('none') }}
          </v-chip>
          <v-chip v-if="filterSettings.state" size="small" closable @click:close="filterSettings.state = ''">
            {{ stateLabel(filterSettings.state) }}
          </v-chip>
        </div>
      </div>
      <div class="clients-table-card__tools">
        <v-text-field
          v-model="quickSearch"
          class="clients-table-card__search"
          density="compact"
          variant="outlined"
          prepend-inner-icon="mdi-magnify"
          :label="$t('search')"
          clearable
          hide-details
        />
        <v-chip class="clients-table-card__count" size="small" variant="tonal" color="primary">
          {{ visibleClients.length }} / {{ clients.length }}
        </v-chip>
      </div>
    </div>
    <v-divider />
    <v-alert v-if="visibleClients.length === 0" type="info" variant="tonal" rounded="lg" :text="$t('noData')" />
    <v-data-table
      v-else-if="!smAndDown"
      :headers="headers"
      :items="visibleClients"
      :hide-default-footer="visibleClients.length<=10"
      :items-per-page="itemPerPage"
      @update:items-per-page="setItemPerPage($event)"
      hide-no-data
      fixed-header
      item-value="name"
      width="100%"
      class="clients-table"
    >
        <template v-slot:item.name="{ item }">
          <div class="clients-identity">
            <div class="clients-identity__avatar">{{ clientInitial(item.name) }}</div>
            <div class="clients-identity__content">
              <strong>{{ item.name }}</strong>
              <span>{{ item.desc || $t('none') }}</span>
              <v-chip v-if="item.group" size="x-small" variant="tonal">{{ item.group }}</v-chip>
            </div>
          </div>
        </template>
        <template v-slot:item.inbounds="{ item }">
          <span class="clients-inbound-count">
          <v-tooltip activator="parent" dir="ltr" location="start" v-if="item.inbounds.length > 0">
            <span v-for="i in item.inbounds">{{ inbounds.find(inb => inb.id == i)?.tag }}<br /></span>
          </v-tooltip>
          <v-icon icon="mdi-tunnel-outline" size="15" />
          {{ item.inbounds?.length }}
          </span>
        </template>
        <template v-slot:item.usage="{ item }">
          <div class="clients-usage" v-tooltip:top="'↓' + HumanReadable.sizeFormat(item.down) + ' - ' + HumanReadable.sizeFormat(item.up) + '↑'">
            <div class="clients-usage__value">
              <strong>{{ HumanReadable.sizeFormat(item.up + item.down) }}</strong>
              <span>/ {{ item.volume == 0 ? $t('unlimited') : HumanReadable.sizeFormat(item.volume) }}</span>
            </div>
            <span>{{ $t('date.expiry') }}: {{ HumanReadable.remainedDays(item.expiry) }}</span>
          </div>
          <v-progress-linear
            class="clients-usage__progress"
            :model-value="percent(item)"
            :color="percentColor(item)"
            v-if="item.volume>0"
            height="4"
            rounded
          >
          </v-progress-linear>
        </template>
        <template v-slot:item.status="{ item }">
          <div class="clients-status">
            <div class="clients-status__primary">
              <span class="clients-status__dot" :class="`clients-status__dot--${clientState(item).color}`"></span>
              <strong>{{ clientState(item).label }}</strong>
            </div>
            <span>{{ isOnline(item.name).value ? $t('online') : $t('client.offline') }}</span>
          </div>
        </template>
        <template v-slot:item.actions="{ item }">
          <div class="clients-table__actions">
            <v-btn size="small" color="primary" variant="tonal" @click="showModal(item.id ?? 0)">
              <v-icon icon="mdi-pencil" start />{{ $t('actions.edit') }}
            </v-btn>
            <v-menu location="bottom end">
              <template v-slot:activator="{ props }">
                <v-btn v-bind="props" size="small" icon="mdi-dots-horizontal" variant="text" :aria-label="$t('client.moreActions')" />
              </template>
              <v-list density="compact" min-width="190">
                <v-list-item prepend-icon="mdi-qrcode" :title="$t('client.qrCode')" @click="showQrCode(item.id ?? 0)" />
                <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-chart-line" :title="$t('stats.graphTitle')" @click="showStats(item.name)" />
                <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-history" :title="$t('client.history')" @click="showHistory(item.id)" />
                <v-divider />
                <v-list-item prepend-icon="mdi-delete" base-color="error" :title="$t('actions.del')" @click="requestDelete(item)" />
              </v-list>
            </v-menu>
          </div>
        </template>
    </v-data-table>

    <div v-else class="clients-mobile-list">
      <v-card v-for="item in mobileClients" :key="item.id" class="clients-mobile-card" rounded="xl" variant="flat">
        <div class="clients-mobile-card__head">
          <div class="clients-identity">
            <div class="clients-identity__avatar">{{ clientInitial(item.name) }}</div>
            <div class="clients-identity__content">
              <strong>{{ item.name }}</strong>
              <span>{{ item.desc || $t('none') }}</span>
            </div>
          </div>
          <v-chip size="small" :color="clientState(item).color" variant="tonal">{{ clientState(item).label }}</v-chip>
        </div>
        <div class="clients-mobile-card__tags">
          <v-chip size="x-small" variant="tonal">{{ item.group || $t('none') }}</v-chip>
          <v-chip size="x-small" variant="outlined">{{ $t('pages.inbounds') }} {{ item.inbounds?.length ?? 0 }}</v-chip>
          <v-chip size="x-small" :color="isOnline(item.name).value ? 'success' : undefined" variant="outlined">
            {{ isOnline(item.name).value ? $t('online') : $t('client.offline') }}
          </v-chip>
        </div>
        <div class="clients-mobile-card__metrics">
          <div><span>{{ $t('stats.usage') }}</span><strong>{{ HumanReadable.sizeFormat(item.up + item.down) }}</strong></div>
          <div><span>{{ $t('stats.volume') }}</span><strong>{{ item.volume === 0 ? $t('unlimited') : HumanReadable.sizeFormat(item.volume) }}</strong></div>
          <div><span>{{ $t('date.expiry') }}</span><strong>{{ HumanReadable.remainedDays(item.expiry) }}</strong></div>
        </div>
        <v-progress-linear v-if="item.volume > 0" :model-value="percent(item)" :color="percentColor(item)" rounded />
        <div class="clients-mobile-card__actions">
          <v-btn color="primary" variant="tonal" @click="showModal(item.id ?? 0)"><v-icon icon="mdi-pencil" start />{{ $t('actions.edit') }}</v-btn>
          <v-menu location="top end">
            <template v-slot:activator="{ props }">
              <v-btn v-bind="props" variant="outlined"><v-icon icon="mdi-dots-horizontal" start />{{ $t('client.moreActions') }}</v-btn>
            </template>
            <v-list density="compact" min-width="190">
              <v-list-item prepend-icon="mdi-qrcode" :title="$t('client.qrCode')" @click="showQrCode(item.id ?? 0)" />
              <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-chart-line" :title="$t('stats.graphTitle')" @click="showStats(item.name)" />
              <v-list-item v-if="Data().enableTraffic" prepend-icon="mdi-history" :title="$t('client.history')" @click="showHistory(item.id)" />
              <v-divider />
              <v-list-item prepend-icon="mdi-delete" base-color="error" :title="$t('actions.del')" @click="requestDelete(item)" />
            </v-list>
          </v-menu>
        </div>
      </v-card>
      <v-pagination v-if="mobilePageCount > 1" v-model="mobilePage" :length="mobilePageCount" :total-visible="3" rounded="circle" />
    </div>
  </v-card>
</template>
<style>
.clients-hero,
.clients-table-card {
  margin-top: 16px;
  padding: 18px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.22), transparent 28%),
    var(--np-surface);
  border: 1px solid var(--np-border);
  box-shadow: var(--np-shadow);
  backdrop-filter: blur(28px) saturate(1.12);
}

.clients-hero {
  overflow: hidden;
}

.clients-hero__topline {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 10px;
}

.clients-hero__badge {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--np-accent);
  background: rgba(10, 132, 255, 0.08);
}

.clients-hero__badge--soft {
  text-transform: none;
  letter-spacing: 0;
  color: var(--np-text-muted);
  background: rgba(148, 163, 184, 0.12);
}

.clients-hero__content {
  min-height: 80px;
}

.clients-hero__title-row {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}

.clients-hero__icon {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 16px;
  color: var(--np-accent);
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.16), rgba(14, 165, 233, 0.08));
}

.clients-hero__title {
  margin: 0;
  font-size: clamp(28px, 3vw, 40px);
  line-height: 1.1;
  font-weight: 800;
  letter-spacing: -0.03em;
}

.clients-hero__subtitle {
  display: none;
}

.clients-hero__meta {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  color: var(--np-text-muted);
  font-size: 13px;
}

.clients-hero__actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.clients-hero__primary {
  min-width: 132px;
}

.clients-hero__icon-btn {
  width: 44px;
  height: 44px;
  min-width: 44px;
  border-radius: 999px;
}

.clients-filter {
  min-width: min(92vw, 360px);
}

.clients-filter__container {
  padding: 8px 16px 0;
}

.clients-filter__actions {
  padding: 0 16px 16px;
}

.clients-table-card {
  overflow: hidden;
}

.clients-table-card__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.clients-table-card__title {
  font-size: 1.02rem;
  font-weight: 700;
}

.clients-table-card__subtitle {
  margin-top: 6px;
  color: var(--np-text-muted);
  line-height: 1.6;
}

.clients-table-card__filters {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.clients-table-card__tools {
  display: flex;
  min-width: min(100%, 340px);
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.clients-table-card__search {
  min-width: 220px;
}

.clients-table-card__count {
  min-width: 48px;
  justify-content: center;
  font-weight: 800;
}

.clients-table {
  width: 100%;
  margin-top: 4px;
}

.clients-table .v-table__wrapper {
  overflow-x: auto;
  scrollbar-gutter: stable both-edges;
}

.clients-table .v-table__wrapper table {
  min-width: 760px;
  border-collapse: separate;
  border-spacing: 0 7px;
}

.clients-table thead th {
  white-space: nowrap;
  height: 38px !important;
  border-bottom: 0 !important;
  background: transparent;
  color: var(--np-text-main);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.clients-table tbody td {
  height: 66px !important;
  border-top: 1px solid var(--np-border) !important;
  border-bottom: 1px solid var(--np-border) !important;
  background: color-mix(in srgb, var(--np-surface-muted) 82%, transparent);
  color: var(--np-text-main);
  transition: background 160ms ease, border-color 160ms ease;
}

.clients-table tbody td:first-child {
  border-left: 1px solid var(--np-border) !important;
  border-radius: 16px 0 0 16px;
}

.clients-table tbody td:last-child {
  border-right: 1px solid var(--np-border) !important;
  border-radius: 0 16px 16px 0;
}

.clients-table tbody tr:hover td {
  border-color: rgba(10, 132, 255, 0.2) !important;
  background: rgba(10, 132, 255, 0.07);
}

.clients-table__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  min-width: 0;
}

.clients-identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.clients-identity__avatar {
  display: grid;
  width: 40px;
  height: 40px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid rgba(10, 132, 255, 0.16);
  border-radius: 14px;
  color: var(--np-accent);
  background:
    linear-gradient(145deg, rgba(125, 211, 252, 0.2), rgba(59, 130, 246, 0.08)),
    var(--np-surface);
  font-weight: 800;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.2);
}

.clients-identity__content {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
}

.clients-identity__content strong,
.clients-identity__content span {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clients-identity__content span {
  color: var(--np-text-muted);
  font-size: 0.75rem;
}

.clients-usage,
.clients-status {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.clients-usage__value {
  display: flex;
  align-items: baseline;
  gap: 4px;
  white-space: nowrap;
}

.clients-usage__value strong {
  color: var(--np-text-main);
  font-size: 0.84rem;
}

.clients-usage__value span {
  color: var(--np-text-muted);
  font-size: 0.74rem;
}

.clients-usage__progress {
  width: min(100%, 210px);
  margin-top: 7px;
}

.clients-inbound-count {
  display: inline-flex;
  min-width: 48px;
  height: 30px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid var(--np-border);
  border-radius: 11px;
  color: var(--np-text-main);
  background: var(--np-surface-muted);
  font-weight: 700;
}

.clients-inbound-count .v-icon {
  color: var(--np-accent);
}

.clients-status__primary {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.clients-status__primary strong {
  font-size: 0.82rem;
}

.clients-status__dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: rgb(148, 163, 184);
  box-shadow: 0 0 0 4px rgba(148, 163, 184, 0.12);
}

.clients-status__dot--success {
  background: rgb(34, 197, 94);
  box-shadow: 0 0 0 4px rgba(34, 197, 94, 0.12);
}

.clients-status__dot--error {
  background: rgb(239, 68, 68);
  box-shadow: 0 0 0 4px rgba(239, 68, 68, 0.12);
}

.clients-usage > span,
.clients-status > span {
  color: var(--np-text-muted);
  font-size: 0.74rem;
}

.clients-mobile-list {
  display: grid;
  gap: 12px;
  padding-top: 14px;
}

.clients-mobile-card {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--np-border);
  background: var(--np-surface-muted) !important;
  box-shadow: none;
}

.clients-mobile-card__head {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.clients-mobile-card__tags,
.clients-mobile-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.clients-mobile-card__metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.clients-mobile-card__metrics > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
  padding: 9px;
  border-radius: 12px;
  background: var(--np-surface);
}

.clients-mobile-card__metrics span {
  color: var(--np-text-muted);
  font-size: 0.7rem;
}

.clients-mobile-card__metrics strong {
  overflow: hidden;
  font-size: 0.8rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.clients-mobile-card__actions .v-btn {
  flex: 1 1 calc(50% - 4px);
}

.clients-table .v-data-table-footer {
  flex-wrap: wrap;
  gap: 6px;
  min-height: 52px;
}

.v-theme--dark .clients-hero,
.v-theme--dark .clients-table-card {
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), transparent 24%),
    rgba(11, 18, 31, 0.78);
  border-color: rgba(148, 163, 184, 0.16);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.3);
}

.v-theme--dark .clients-hero__badge--soft {
  background: rgba(148, 163, 184, 0.16);
  color: rgba(237, 244, 255, 0.76);
}

.v-theme--dark .clients-table thead th {
  background: transparent;
  color: rgba(237, 244, 255, 0.92);
}

.v-theme--dark .clients-table tbody td {
  border-color: rgba(148, 163, 184, 0.13) !important;
  background: rgba(15, 23, 38, 0.72);
  color: rgba(237, 244, 255, 0.94);
}

.v-theme--dark .clients-table tbody tr:hover td {
  border-color: rgba(125, 211, 252, 0.22) !important;
  background: rgba(30, 58, 92, 0.5);
}

@media (max-width: 960px) {
  .clients-hero,
  .clients-table-card {
    padding: 16px;
  }

  .clients-hero__actions {
    justify-content: flex-start;
  }

}

@media (max-width: 600px) {
  .clients-hero__title-row {
    gap: 12px;
  }

  .clients-hero__icon {
    width: 46px;
    height: 46px;
  }

  .clients-hero__title {
    font-size: 24px;
  }

  .clients-table-card__head {
    flex-direction: column;
    gap: 10px;
  }

  .clients-table-card__tools {
    width: 100%;
    min-width: 0;
    justify-content: space-between;
  }

  .clients-table-card__search {
    min-width: 0;
    flex: 1 1 auto;
  }

  .clients-table-card__subtitle {
    display: none;
  }
}
</style>
<script lang="ts" setup>
import Data from '@/store/modules/data'
import { Client } from '@/types/clients'
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { HumanReadable } from '@/plugins/utils'
import { i18n } from '@/locales'
import { useDisplay } from 'vuetify'

const ClientModal = defineAsyncComponent(() => import('@/layouts/modals/Client.vue'))
const ClientAddBulk = defineAsyncComponent(() => import('@/layouts/modals/ClientAddBulk.vue'))
const ClientEditBulk = defineAsyncComponent(() => import('@/layouts/modals/ClientEditBulk.vue'))
const ClientHistory = defineAsyncComponent(() => import('@/layouts/modals/ClientHistory.vue'))
const QrCode = defineAsyncComponent(() => import('@/layouts/modals/QrCode.vue'))
const Stats = defineAsyncComponent(() => import('@/layouts/modals/Stats.vue'))

const { smAndDown } = useDisplay()

const clients = computed((): any[] => {
  return Data().clients
})

const quickSearch = ref('')

const visibleClients = computed((): Client[] => {
  const query = quickSearch.value.trim().toLowerCase()
  return clients.value.filter((client: Client) => {
    if (query && ![client.name, client.desc, client.group].some(value => String(value ?? '').toLowerCase().includes(query))) return false
    if (filterSettings.value.group !== '-' && client.group !== filterSettings.value.group) return false
    switch (filterSettings.value.state) {
      case 'disable': return !client.enable
      case 'expired': return client.expiry > 0 && client.expiry < Date.now() / 1000
      case 'online': return Data().onlines?.user?.includes(client.name) ?? false
      default: return true
    }
  })
})

const onlineCount = computed((): number => {
  return Data().onlines?.user?.length ?? 0
})

const isOnline = (cname: string) => computed(() => {
  return Data().onlines?.user ? Data().onlines.user.includes(cname) : false
})

const inbounds = computed((): any[] => {
  return Data().inbounds?? []
})

const inboundTags = computed((): any[] => {
  if (!inbounds.value) return []
  return inbounds.value?.filter(i => i.tag != "" && i.users).map(i => { return { title: i.tag, value: i.id } })
})

const groups = computed((): string[] => {
  if (!clients.value) return []
  return Array.from(new Set(clients.value?.map(c => c.group)))
})

const actionMenu = ref(false)
const filterMenu = ref(false)
const filterSettings = ref({
  state: '',
  group: '-',
})

const activeFilterCount = computed(() =>
  Number(quickSearch.value.trim().length > 0)
  + Number(filterSettings.value.group !== '-')
  + Number(filterSettings.value.state !== ''),
)

const filterItems = [
  { title: i18n.global.t('none'), value: '' },
  { title: i18n.global.t('disable'), value: 'disable' },
  { title: i18n.global.t('date.expired'), value: 'expired' },
  { title: i18n.global.t('online'), value: 'online' },
]

const headers = [
  { title: i18n.global.t('client.name'), key: 'name' },
  { title: i18n.global.t('pages.inbounds'), key: 'inbounds', width: 10 },
  { title: i18n.global.t('stats.usage'), key: 'usage', sortable: false },
  { title: i18n.global.t('status'), key: 'status', sortable: false },
  { title: i18n.global.t('actions.action'), key: 'actions', sortable: false, align: 'end' as const },
]

const itemPerPage = ref(localStorage.getItem('items-per-page') || '10')

const setItemPerPage = (items: number) => {
  itemPerPage.value = items.toString()
  localStorage.setItem('items-per-page', items.toString())
}

const mobilePage = ref(1)
const mobilePageSize = 10
const mobilePageCount = computed(() => Math.max(1, Math.ceil(visibleClients.value.length / mobilePageSize)))
const mobileClients = computed(() => {
  const start = (mobilePage.value - 1) * mobilePageSize
  return visibleClients.value.slice(start, start + mobilePageSize)
})

watch(visibleClients, () => {
  mobilePage.value = 1
})

const modal = ref({
  visible: false,
  id: 0,
})

const deleteDialog = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<Client | null>(null)

const showModal = async (id: number) => {
  modal.value.id = id
  modal.value.visible = true
}
const closeModal = () => {
  modal.value.visible = false
}

const requestDelete = (client: Client) => {
  deleteTarget.value = client
  deleteDialog.value = true
}

const confirmDelete = async () => {
  if (!deleteTarget.value?.id) return
  deleteLoading.value = true
  try {
    const success = await Data().save('clients', 'del', deleteTarget.value.id)
    if (success) {
      deleteDialog.value = false
      deleteTarget.value = null
    }
  } finally {
    deleteLoading.value = false
  }
}

const qrcode = ref({
  visible: false,
  id: 0,
})

const showQrCode = (id: number) => {
  qrcode.value.id = id
  qrcode.value.visible = true
}
const closeQrCode = () => {
  qrcode.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "user",
  tag: "",
})

const historyModal = ref({
  visible: false,
  id: 0,
})

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}

const showHistory = (id?: number) => {
  if (!id) return
  historyModal.value.id = id
  historyModal.value.visible = true
}

const closeHistory = () => {
  historyModal.value.visible = false
}

const doFilter = () => {
  filterMenu.value = false
}

const clearFilter = () => {
  quickSearch.value = ''
  filterSettings.value = {
    state: '',
    group: '-',
  }
  filterMenu.value = false
}

const stateLabel = (state: string) => filterItems.find(item => item.value === state)?.title ?? state

const clientInitial = (name: string) => (name?.trim().charAt(0) || '?').toUpperCase()

const clientState = (client: Client) => {
  if (!client.enable) return { label: i18n.global.t('disable'), color: 'secondary' }
  if (client.expiry > 0 && client.expiry <= Date.now() / 1000) return { label: i18n.global.t('date.expired'), color: 'error' }
  if (client.volume > 0 && client.up + client.down >= client.volume) return { label: i18n.global.t('client.exhausted'), color: 'error' }
  return { label: i18n.global.t('enable'), color: 'success' }
}

const addBulkModal = ref(false)

const addBulk = () => {
  addBulkModal.value = true
  actionMenu.value = false
}

const closeAddBulk = () => {
  addBulkModal.value = false
}

const editBulkModal = ref(false)

const editBulk = () => {
  editBulkModal.value = true
  actionMenu.value = false
}

const closeEditBulk = () => {
  editBulkModal.value = false
}

const percent = (c: Client) => { return c.volume>0 ? Math.round((c.up+c.down) *100 / c.volume) : 0 }
const percentColor = (c: Client) => { return (c.up+c.down) >= c.volume ? 'error' : percent(c)>90 ? 'warning' : 'success' }

</script>
