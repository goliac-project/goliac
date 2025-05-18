<template>
  <el-breadcrumb separator="/">
    <el-breadcrumb-item :to="{ path: '/' }">Goliac</el-breadcrumb-item>
    <el-breadcrumb-item :to="{ path: '/' }">dashboard</el-breadcrumb-item>
  </el-breadcrumb>
  <el-divider />

  <el-row>
    <el-col :span="20" :offset="2">
      <el-row>
        <el-tabs class="full-width-tabs" v-model="activeTabName">
          <el-tab-pane label="Status" name="status">
            <el-table
              :data="statusTable"
              :stripe="true"
              :highlight-current-row="false"
              :default-sort="{ prop: 'title', order: 'descending' }"
            >
              <el-table-column width="250" prop="key" align="left" label="Key" sortable />
              <el-table-column prop="value" align="left" label="Value" />
            </el-table>
          </el-tab-pane>
          <el-tab-pane label="Statistics" name="statistics">
            <el-table
              :data="statisticsTable"
              :stripe="true"
              :highlight-current-row="false"
              :default-sort="{ prop: 'title', order: 'descending' }"
            >
              <el-table-column width="250" prop="key" align="left" label="Key" sortable />
              <el-table-column prop="value" align="left" label="Value" />
            </el-table>
          </el-tab-pane>
          <el-tab-pane label="Unmanaged" name="unmanaged">
            <el-table
              :data="unmanagedTable"
              :stripe="true"
              :highlight-current-row="false"
              :default-sort="{ prop: 'title', order: 'descending' }"
            >
              <el-table-column width="250" prop="key" align="left" label="Key" sortable />
              <el-table-column width="100" prop="nb" align="left" label="Nb" />
              <el-table-column prop="values" align="left" label="Values" />
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </el-row>
      <el-row v-if="detailedErrors.length > 0 || detailedWarnings.length > 0">
        <el-divider />
      </el-row>
      <el-row v-if="detailedErrors.length > 0">
        <el-table
            :data="detailedErrors"
            :stripe="true"
            :highlight-current-row="false"
        >
            <el-table-column prop="Errors" align="left" label="Errors">
                <template #default="{row}">
                    <span>{{ row }}</span>
                </template>
            </el-table-column>
        </el-table>
      </el-row>
      <el-row v-if="detailedWarnings.length > 0">
        <el-table
            :data="detailedWarnings"
            :stripe="true"
            :highlight-current-row="false"
        >
            <el-table-column prop="Warnings" align="left" label="Warnings">
                <template #default="{row}">
                    <span>{{ row }}</span>
                </template>
            </el-table-column>
        </el-table>
      </el-row>
      <el-row>
        <el-divider />
      </el-row>
      <el-row>
        <div class="flex-container">
            <el-text style="color:red;">Force a Github sync ? </el-text>
            <span> &nbsp; </span>
            <el-button @click="resync">Re-sync</el-button>
        </div>
      </el-row>
      <el-row>
        <span> &nbsp; </span>
      </el-row>
      <el-row>
        <div class="flex-container">
            <el-text style="color:red;">Invalidate the cache of remote Github objects? </el-text>
            <span> &nbsp; </span>
            <el-button @click="flushcache">Flush cache</el-button>
        </div>
      </el-row>
      <el-row>
        <el-divider />
      </el-row>
      <el-row>
        <div class="flex-container">
            <el-text>Build version: {{ version }}</el-text>
        </div>
      </el-row>
    </el-col>
  </el-row>
</template>
  
<script>
  import Axios from "axios";
  
  import constants from "@/constants";
  import helpers from "@/helpers/helpers";
  import { h } from 'vue'
  import { ElNotification } from 'element-plus'
  
  const { handleErr } = helpers;
  
  const { API_URL } = constants;
  
  export default {
    name: "DashboardApp",
    components: {
    },
    data() {
      return {
        flushcacheVisible: false,
        statusTable: [],
        statisticsTable: [],
        unmanagedTable: [],
        detailedErrors: [],
        detailedWarnings: [],
        version: "",
        activeTabName: "status",
      };
    },
    mounted() {
      this.getStatus()
      this.getStatistics()
      this.getUnmanaged()

      setInterval(() => {
        this.getStatus()
        this.getStatistics()
        this.getUnmanaged()
      }, 60000);
    },
    beforeUnmount() {
      clearInterval(this.interval);
    },
    methods: {
      getUnmanaged() {
          Axios.get(`${API_URL}/unmanaged`).then(response => {
                let unmanaged = response.data;
                let userNext = "";
                if (unmanaged.users && unmanaged.users.length > 20) {
                  userNext = ", ...";
                }
                let externallyNext = "";
                if (unmanaged.externally_managed_teams && unmanaged.externally_managed_teams.length > 20) {
                  externallyNext = ", ...";
                }
                let teamsNext = "";
                if (unmanaged.teams && unmanaged.teams.length > 20) {
                  teamsNext = ", ...";
                }
                let reposNext = "";
                if (unmanaged.repos && unmanaged.repos.length > 20) {
                  reposNext = ", ...";
                }
                let rulesetsNext = "";
                if (unmanaged.rulesets && unmanaged.rulesets.length > 20) {
                  rulesetsNext = ", ...";
                }
                this.unmanagedTable = [
                    {
                        key: "Unmanaged Users",
                        nb: unmanaged.users ? unmanaged.users.length : "unknown",
                        values: unmanaged.users ? unmanaged.users.slice(0, 20).join(",") + userNext : "unknown",
                    },
                    {
                        key: "Externally Managed Teams",
                        nb: unmanaged.externally_managed_teams ? unmanaged.externally_managed_teams.length : "unknown",
                        values: unmanaged.externally_managed_teams ? unmanaged.externally_managed_teams.slice(0, 20).join(",") + externallyNext : "unknown",
                    },
                    {
                        key: "Unmanaged Teams",
                        nb: unmanaged.teams ? unmanaged.teams.length : "unknown",
                        values: unmanaged.teams ? unmanaged.teams.slice(0, 20).join(",") + teamsNext : "unknown",
                    },
                    {
                        key: "Unmanaged Repositories",
                        nb: unmanaged.repos ? unmanaged.repos.length : "unknown",
                        values: unmanaged.repos ? unmanaged.repos.slice(0, 20).join(",") + reposNext : "unknown",
                    },
                    {
                        key: "Unmanaged Rulesets",
                        nb: unmanaged.rulesets ? unmanaged.rulesets.length : "unknown",
                        values: unmanaged.rulesets ? unmanaged.rulesets.slice(0, 20).join(",") + rulesetsNext : "unknown",
                    },
                ]
          }, handleErr.bind(this));
        },
      getStatistics() {
          Axios.get(`${API_URL}/statistics`).then(response => {
                let statistics = response.data;
                this.statisticsTable = [
                    {
                        key: "Last Duration to Apply",
                        value: statistics.lastTimeToApply,
                    },
                    {
                        key: "Last Number of Github API Calls",
                        value: statistics.lastGithubApiCalls,
                    },
                    {
                      key: "Last Number of Github API Throttled",
                        value: statistics.lastGithubThrottled,
                    },
                    {
                        key: "Max Duration to Apply",
                        value: statistics.maxTimeToApply,
                    },
                    {
                        key: "Max Github API Calls per apply",
                        value: statistics.maxGithubApiCalls,
                    },
                    {
                      key: "Max Github API Throttled per apply",
                        value: statistics.maxGithubThrottled,
                    },
                ]
          }, handleErr.bind(this));
        },
        getStatus() {
          Axios.get(`${API_URL}/status`).then(response => {
                let status = response.data;
                this.version = status.version;
                this.detailedErrors = status.detailedErrors;
                this.detailedWarnings = status.detailedWarnings;
                this.statusTable = [
                    {
                        key: "Last Sync",
                        value: status.lastSyncTime+" (UTC)",
                    },
                    {
                        key: "Last Sync Error",
                        value: status.lastSyncError,
                    },
                    {
                        key: "Nb Users",
                        value: status.nbUsers,
                    },
                    {
                        key: "Nb External Users",
                        value: status.nbUsersExternal,
                    },
                    {
                        key: "Nb Managed Teams",
                        value: status.nbTeams,
                    },
                    {
                        key: "Nb Managed Repositories",
                        value: status.nbRepos
                    },
                ]
          }, handleErr.bind(this));
        },
        flushcache() {
          Axios.post(`${API_URL}/flushcache`,{}).then(() => {
            ElNotification({
                title: 'Cache flushed',
                message: h('i', { style: 'color: teal' }, 'Github cache objects flushed'),
            })          
          }, handleErr.bind(this));
        },
        resync() {
          Axios.post(`${API_URL}/resync`,{}).then(() => {
            ElNotification({
                title: 'Sync started',
                message: h('i', { style: 'color: teal' }, 'Refresh this page in a moment'),
            })          
          }, handleErr.bind(this));
        },
    }
  };
</script>

<style scoped>
.full-width-tabs {
  width: 100%; /* or a specific width like 80vw or 1200px */
}
</style>
