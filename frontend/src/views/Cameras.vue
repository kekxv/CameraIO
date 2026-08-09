<template>
  <div>
    <!-- 页头 -->
    <div class="ui-page-header">
      <div>
        <h1 class="ui-page-title">摄像头管理</h1>
        <p class="ui-page-description">管理所有接入的监控摄像头</p>
      </div>
      <div class="ui-page-header-actions compat-flex-gap-2">
        <el-button
          @click="handleScanLAN"
          plain
        >
          <AppIcon name="scan" class="w-4 h-4" />
          <span>扫描局域网</span>
        </el-button>
        <el-button
          @click="showAddDialog = true"
          type="primary"
        >
          + 添加摄像头
        </el-button>
      </div>
    </div>

    <!-- 加载状态 -->
    <el-alert v-if="loading" title="加载中..." type="info" :closable="false" class="mb-4" />

    <!-- 空状态 -->
    <div v-else-if="cameras.length === 0" class="py-16">
      <el-empty description="还没有添加摄像头">
        <el-button type="primary" @click="showAddDialog = true">添加第一个摄像头</el-button>
      </el-empty>
    </div>

    <!-- 摄像头列表 -->
    <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <el-card
        v-for="cam in cameras"
        :key="cam.id"
        shadow="never"
        class="flex flex-col"
      >
        <div class="p-4 flex-1">
          <div class="flex items-start justify-between">
            <div class="flex-1 min-w-0">
              <h3 class="font-semibold text-slate-800 truncate">{{ cam.name }}</h3>
              <p class="text-xs text-slate-500 mt-0.5">{{ cam.ip }}:{{ cam.port }}</p>
            </div>
            <el-tag :type="cam.status === 'online' ? 'success' : 'info'" effect="plain" size="small">
              {{ cam.status === 'online' ? '在线' : '离线' }}
            </el-tag>
          </div>

          <div class="mt-3 space-y-1 text-xs text-slate-500">
            <!-- GB28181 卡片：显示国标信息 -->
            <template v-if="cam.access_protocol === 'gb28181'">
              <div class="flex items-center gap-1">
                <span class="px-1.5 py-0.5 bg-blue-100 text-blue-700 rounded text-[10px] font-medium">GB28181</span>
                <span v-if="cam.transport" class="px-1.5 py-0.5 rounded text-[10px] font-medium"
                  :class="cam.transport === 'TCP' ? 'bg-cyan-100 text-cyan-700' : 'bg-slate-100 text-slate-600'">
                  {{ cam.transport }}
                </span>
                <span class="ml-auto px-1.5 py-0.5 rounded text-[10px] font-medium"
                  :class="cam.status === 'online' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500'">
                  {{ cam.status === 'online' ? '已注册' : '未注册' }}
                </span>
              </div>
              <div class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">设备编码:</span>
                <span class="font-mono truncate" :title="cam.device_id">{{ cam.device_id || '-' }}</span>
              </div>
              <div class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">通道编码:</span>
                <span class="font-mono truncate" :title="cam.channel_id">{{ cam.channel_id || cam.device_id || '-' }}</span>
              </div>
              <div v-if="cam.ip" class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">注册地址:</span>
                <span class="font-mono">{{ cam.ip }}{{ cam.port ? ':' + cam.port : '' }}</span>
              </div>
              <div v-if="cam.last_time_sync" class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">心跳/对时:</span>
                <span>{{ formatTime(cam.last_time_sync) }}</span>
              </div>
            </template>

            <!-- RTSP/本地卡片：显示品牌/编码/流信息 -->
            <template v-else>
              <div class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">品牌:</span>
                <span class="truncate">{{ brandLabel(cam.brand) }}</span>
                <span v-if="cam.device_type && cam.device_type !== 'ipc'" class="px-1.5 py-0.5 bg-purple-100 text-purple-700 rounded text-[10px] font-medium flex-shrink-0">
                  {{ deviceTypeLabel(cam.device_type) }}
                </span>
                <!-- 编码格式 radio-button 组 -->
                <el-radio-group
                  :model-value="cam.preferred_codec || 'auto'"
                  size="small"
                  class="ml-auto flex-shrink-0"
                  aria-label="切换编码格式"
                  @change="handleSetCodec(cam, $event)"
                >
                  <el-radio-button
                    v-for="opt in ['auto', 'h264', 'h265']"
                    :key="opt"
                    :value="opt"
                  >
                    {{ opt === 'auto' ? '自动' : opt === 'h264' ? 'H.264' : 'H.265' }}
                  </el-radio-button>
                </el-radio-group>
              </div>
              <div v-if="cam.nvr_channel > 0" class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">通道:</span>
                <span>CH{{ cam.nvr_channel }}</span>
                <span class="px-1.5 py-0.5 rounded text-[10px] font-medium ml-1"
                  :class="(cam.stream_type || 'main') === 'sub' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'">
                  {{ (cam.stream_type || 'main') === 'sub' ? '子码流' : '主码流' }}
                </span>
              </div>
              <div v-else class="flex items-center gap-1">
                <span class="px-1.5 py-0.5 rounded text-[10px] font-medium"
                  :class="(cam.stream_type || 'main') === 'sub' ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'">
                  {{ (cam.stream_type || 'main') === 'sub' ? '子码流' : '主码流' }}
                </span>
              </div>
              <div class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">RTSP:</span>
                <span class="font-mono truncate min-w-0" :title="cam.rtsp_url">{{ cam.rtsp_url }}</span>
              </div>
              <div v-if="cam.resolution || cam.codec" class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">视频:</span>
                <span v-if="cam.resolution" class="font-mono">{{ cam.resolution }}</span>
                <span v-if="cam.codec" class="px-1.5 py-0.5 rounded text-[10px] font-medium"
                  :class="cam.codec === 'H.265' ? 'bg-purple-100 text-purple-700' : 'bg-blue-100 text-blue-700'">
                  {{ cam.codec }}
                </span>
              </div>
              <div v-if="cam.last_time_sync" class="flex items-center gap-1">
                <span class="text-slate-400 flex-shrink-0">对时:</span>
                <span>{{ formatTime(cam.last_time_sync) }}</span>
              </div>
            </template>

            <!-- 错误信息展示 -->
            <el-alert v-if="cam.last_error" :title="cam.last_error" type="warning" :closable="false" class="mt-2" />
          </div>
        </div>

        <div class="px-4 py-3 bg-slate-50 border-t border-slate-100 compat-flex-gap-2">
          <!-- RTSP: 同步时间/测试/网络配置 -->
          <template v-if="cam.access_protocol !== 'gb28181'">
            <el-button
              plain
              size="small"
              class="flex-1"
              @click="handleSyncTime(cam)"
              :disabled="syncingId === cam.id"
            >
              <span v-if="syncingId === cam.id">同步中...</span>
              <template v-else>
                <AppIcon name="clock" class="w-3.5 h-3.5" />
                <span>同步时间</span>
              </template>
            </el-button>
            <el-tooltip content="测试连接">
              <el-button
                plain
                circle
                size="small"
                aria-label="测试连接"
                :loading="testingId === cam.id"
                @click="handleTest(cam)"
              >
                <AppIcon name="plug" class="w-3.5 h-3.5" />
              </el-button>
            </el-tooltip>
            <el-tooltip content="网络配置">
              <el-button plain circle size="small" aria-label="网络配置" @click="showNetworkDialog(cam)">
                <AppIcon name="globe" class="w-3.5 h-3.5" />
              </el-button>
            </el-tooltip>
          </template>
          <!-- GB28181: 显示注册状态（无同步/测试/网络） -->
          <el-tooltip content="编辑">
            <el-button plain circle size="small" aria-label="编辑" @click="handleEdit(cam)">
              <AppIcon name="edit" class="w-3.5 h-3.5" />
            </el-button>
          </el-tooltip>
          <el-tooltip content="删除">
            <el-button plain circle size="small" type="danger" aria-label="删除" @click="handleDelete(cam)">
              <AppIcon name="trash" class="w-3.5 h-3.5" />
            </el-button>
          </el-tooltip>
        </div>
      </el-card>
    </div>

    <!-- 添加/编辑对话框 -->
    <el-dialog v-model="cameraDialogOpen" :title="editingCamera ? '编辑摄像头' : '添加摄像头'" width="680px" class="camera-dialog">
        <el-form @submit.prevent="handleSubmit" class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">名称 *</label>
            <el-input
              v-model="form.name"
              type="text"
              required
              placeholder="Front Gate"
            />
          </div>

          <!-- 接入协议（放在名称下方） -->
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-1">接入协议</label>
            <div class="grid grid-cols-3 gap-2">
              <el-radio-group v-model="form.access_protocol" class="grid grid-cols-3 gap-2">
              <el-radio
                value="rtsp"
                class="flex items-center gap-2 px-3 py-2 border rounded-md cursor-pointer transition-colors"
                :class="form.access_protocol === 'rtsp'
                  ? 'border-primary-500 bg-primary-50 text-primary-700'
                  : 'border-slate-300 hover:bg-slate-50'"
              >
                <div>
                  <div class="text-sm font-medium">RTSP</div>
                  <div class="text-xs text-slate-500">主动拉流</div>
                </div>
              </el-radio>
              <el-radio
                value="gb28181"
                class="flex items-center gap-2 px-3 py-2 border rounded-md cursor-pointer transition-colors"
                :class="form.access_protocol === 'gb28181'
                  ? 'border-primary-500 bg-primary-50 text-primary-700'
                  : 'border-slate-300 hover:bg-slate-50'"
              >
                <div>
                  <div class="text-sm font-medium">GB28181</div>
                  <div class="text-xs text-slate-500">国标 SIP</div>
                </div>
              </el-radio>
              <el-radio
                value="local"
                class="flex items-center gap-2 px-3 py-2 border rounded-md cursor-pointer transition-colors"
                :class="form.access_protocol === 'local'
                  ? 'border-primary-500 bg-primary-50 text-primary-700'
                  : 'border-slate-300 hover:bg-slate-50'"
              >
                <div>
                  <div class="text-sm font-medium">本地</div>
                  <div class="text-xs text-slate-500">USB/系统</div>
                </div>
              </el-radio>
              </el-radio-group>
          </div>
          </div>

          <!-- RTSP 设备的网络配置（GB28181 通过 SIP 注册，本地用系统设备，均无需 IP/端口） -->
          <template v-if="form.access_protocol === 'rtsp'">
            <div class="grid grid-cols-3 gap-3">
              <div class="col-span-2">
                <label class="block text-sm font-medium text-slate-700 mb-1">IP 地址 *</label>
                <el-input
                  v-model="form.ip"
                  type="text"
                  required
                  placeholder="192.168.1.100"
                />
              </div>
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">端口</label>
                <el-input
                  v-model.number="form.port"
                  type="number"
                  placeholder="554"
                />
              </div>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">用户名</label>
                <el-input
                  v-model="form.username"
                  type="text"
                  placeholder="admin"
                />
              </div>
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">密码</label>
                <el-input
                  v-model="form.password"
                  type="password"
                />
              </div>
            </div>

            <!-- 测试连接按钮 -->
            <div v-if="form.ip && (form.username || form.password)" class="flex items-center gap-2">
              <el-button
                plain
                native-type="button"
                size="small"
                @click="handleTestByIP"
                :loading="testingByIP"
              >
                <AppIcon name="plug" class="w-3.5 h-3.5" />
                <span>测试连接</span>
              </el-button>
              <span v-if="testResult" class="text-xs" :class="testResult.ok ? 'text-emerald-600' : 'text-red-600'">
                {{ testResult.message }}
              </span>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">品牌</label>
                <el-select
                  v-model="form.brand"
                  class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
                >
                  <el-option label="自定义" value="custom" />
                  <el-option label="海康威视" value="hikvision" />
                  <el-option label="宇视" value="uniview" />
                </el-select>
              </div>
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">设备类型</label>
                <el-select
                  v-model="form.device_type"
                  class="w-full px-3 py-2 border border-slate-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary-500"
                >
                  <el-option label="IPC 网络摄像机" value="ipc" />
                  <el-option label="NVR 网络录像机" value="nvr" />
                  <el-option label="DVR 数字录像机" value="dvr" />
                  <el-option label="编码器" value="encoder" />
                </el-select>
              </div>
            </div>
          </template>

          <!-- NVR 通道发现（仅 RTSP，GB28181 用设备/通道编码） -->
          <div v-if="form.access_protocol !== 'gb28181' && (form.device_type === 'nvr' || form.device_type === 'dvr')">
            <div class="flex items-center gap-2 mb-2">
              <el-button
                plain
                native-type="button"
                size="small"
                @click="handleDiscoverChannels"
                :disabled="discovering || !form.ip || !form.username || !form.password"
                :title="!form.ip || !form.username || !form.password ? '请先填写 IP、用户名和密码' : ''"
                :loading="discovering"
              >
                <AppIcon name="search" class="w-3.5 h-3.5" />
                <span>扫描 NVR 通道</span>
              </el-button>
              <span v-if="discoveredChannels.length > 0" class="text-xs text-slate-500">
                发现 {{ discoveredChannels.length }} 个通道，已选 {{ selectedChannels.length }} 个
              </span>
            </div>

            <!-- 通道列表（多选） -->
            <div v-if="discoveredChannels.length > 0" class="border border-slate-200 rounded-md overflow-hidden mb-2 max-h-48 overflow-y-auto">
              <div class="px-3 py-1.5 bg-slate-50 border-b border-slate-200 text-xs font-medium text-slate-600 flex items-center gap-2">
                <el-checkbox
                  :model-value="allChannelsSelected"
                  :indeterminate="selectedChannels.length > 0 && !allChannelsSelected"
                  @change="toggleAllChannels"
                >
                  全选 ({{ selectedChannels.length }}/{{ discoveredChannels.length }})
                </el-checkbox>
              </div>
              <el-checkbox-group v-model="selectedChannels" class="block">
                <el-checkbox
                  v-for="ch in discoveredChannels"
                  :key="ch.channel"
                  :value="ch.channel"
                  class="flex w-full items-center px-3 py-1.5 border-b border-slate-100 last:border-b-0 text-xs"
                >
                  <span class="flex min-w-0 flex-1 items-center gap-2">
                    <span class="font-mono text-slate-600 w-10">CH{{ ch.channel }}</span>
                    <span class="text-slate-500 truncate flex-1">{{ ch.name || ch.profile_token }}</span>
                    <span v-if="ch.rtsp_url" class="text-[10px] text-emerald-600 font-mono truncate max-w-[140px]" :title="ch.rtsp_url">
                      {{ ch.rtsp_url.replace(/^rtsp:\/\/[^@]*@/, 'rtsp://***@') }}
                    </span>
                  </span>
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>

          <!-- RTSP 专属字段 -->
          <template v-if="form.access_protocol === 'rtsp'">
            <!-- 码流选择 -->
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">码流类型</label>
              <el-radio-group v-model="form.stream_type" class="w-full">
                <el-radio-button
                  v-for="opt in [{v:'main',l:'主码流'},{v:'sub',l:'子码流'}]"
                  :key="opt.v"
                  :value="opt.v"
                  class="flex-1"
                >
                  {{ opt.l }}
                </el-radio-button>
              </el-radio-group>
              <p class="text-[10px] text-slate-400 mt-1">
                主码流清晰度高；子码流流畅度高、占用带宽低，适合多路预览
              </p>
            </div>

            <el-checkbox v-model="form.auto_tune_enabled">自动优化编码参数（推荐）</el-checkbox>
          </template>

          <!-- GB28181 专属字段 -->
          <template v-if="form.access_protocol === 'gb28181'">
            <div class="bg-blue-50 border border-blue-200 rounded-md px-3 py-2 text-xs text-blue-700">
              <strong>提示：</strong>配置后，请在摄像头国标设置中填入：
              服务器 IP = CameraIO IP，端口 = 5060，
              服务器 ID = 34020000002000000001，
              服务器域 = 3402000000，
              密码 = 下方设置的密码
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">设备编码 (20 位) *</label>
              <el-input v-model="form.device_id" maxlength="20" required placeholder="34020000001110000001" />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">通道编码 (20 位)</label>
              <el-input v-model="form.channel_id" maxlength="20" placeholder="34020000001320000001" />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">
                密码
                <span class="text-xs text-slate-400 ml-1">国标鉴权密码，需与设备端一致</span>
              </label>
              <el-input v-model="form.password" type="password" show-password placeholder="国标 SIP 鉴权密码" />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">流传输协议</label>
              <el-select v-model="form.transport" class="w-full">
                <el-option label="UDP（推荐，延迟最低）" value="UDP" />
                <el-option label="TCP" value="TCP" />
                <el-option label="TCP/AUTO（自适应）" value="TCP/AUTO" />
              </el-select>
            </div>
          </template>

          <!-- 本地摄像头专属字段 -->
          <template v-if="form.access_protocol === 'local'">
            <div class="bg-amber-50 border border-amber-200 rounded-md px-3 py-2 text-xs text-amber-800">
              <strong>提示：</strong>本地摄像头通过 FFmpeg 直接捕获。
              也可使用 <code class="bg-amber-100 px-1 rounded">localcam serve</code> 工具将其发布为 RTSP 流。
            </div>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">设备索引</label>
                <el-input v-model.number="form.local_index" type="number" :min="-1" placeholder="0" />
              </div>
              <div>
                <label class="block text-sm font-medium text-slate-700 mb-1">设备名称</label>
                <el-input v-model="form.local_name" placeholder="FaceTime HD Camera" />
              </div>
            </div>
            <el-button
              plain
              native-type="button"
              class="w-full"
              @click="scanLocalCameras"
              :loading="scanningLocal"
            >
              <AppIcon name="search" class="w-4 h-4" />
              <span>扫描本机摄像头</span>
            </el-button>
            <div v-if="localCameraList.length > 0" class="border border-slate-200 rounded-md overflow-hidden">
              <el-button
                v-for="cam in localCameraList"
                :key="cam.index"
                text
                class="w-full h-auto justify-between rounded-none border-b border-slate-100 px-3 py-2 last:border-b-0"
                @click="selectLocalCamera(cam)"
              >
                <div>
                  <div class="text-sm font-medium text-slate-800">{{ cam.name }}</div>
                  <div class="text-xs text-slate-500 font-mono">{{ cam.path }} · 索引 {{ cam.index }}</div>
                </div>
                <span class="text-xs text-primary-600">选择 →</span>
              </el-button>
            </div>
          </template>

          <el-alert v-if="submitError" :title="submitError" type="error" :closable="false" />
          <el-alert v-if="submitSuccess" :title="submitSuccess" type="success" :closable="false" />

          <div class="flex justify-end gap-2 pt-2">
            <el-button
              plain
              native-type="button"
              @click="closeDialog"
            >
              取消
            </el-button>
            <!-- 编辑模式：测试按钮（仅 RTSP） -->
            <el-button
              v-if="editingCamera && editingCamera.access_protocol === 'rtsp'"
              plain
              native-type="button"
              @click="handleTest(editingCamera)"
              :loading="testingId === editingCamera.id"
            >
              <AppIcon name="plug" class="w-4 h-4" />
              <span>测试连接</span>
            </el-button>
            <el-button
              type="primary"
              native-type="submit"
              :loading="submitting"
            >
              保存
            </el-button>
          </div>
        </el-form>
    </el-dialog>

    <!-- 测试结果弹窗 -->
    <el-dialog v-model="testInfoDialogOpen" title="设备信息" width="420px" @closed="clearTestInfoDialog">
        <div v-if="testInfoModal" class="space-y-2 text-sm">
          <div v-if="testInfoModal.manufacturer"><span class="text-slate-400">厂商:</span> {{ testInfoModal.manufacturer }}</div>
          <div v-if="testInfoModal.model"><span class="text-slate-400">型号:</span> {{ testInfoModal.model }}</div>
          <div v-if="testInfoModal.firmware_version"><span class="text-slate-400">固件:</span> {{ testInfoModal.firmware_version }}</div>
          <div v-if="testInfoModal.serial_number"><span class="text-slate-400">序列号:</span> <span class="font-mono">{{ testInfoModal.serial_number }}</span></div>
          <div v-if="testInfoModal.hardware_id"><span class="text-slate-400">硬件 ID:</span> {{ testInfoModal.hardware_id }}</div>
          <div v-if="testInfoModal.timezone"><span class="text-slate-400">时区:</span> {{ testInfoModal.timezone }}</div>
          <div v-if="testInfoModal.permission_note" class="mt-2 text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded px-2 py-1.5 flex items-start gap-1.5">
            <AppIcon name="warning" class="w-3.5 h-3.5 mt-px flex-shrink-0" />
            <span>{{ testInfoModal.permission_note }}</span>
          </div>
        </div>
        <div class="mt-4 flex justify-end">
          <el-button plain @click="closeTestInfoDialog">关闭</el-button>
        </div>
    </el-dialog>

    <!-- 局域网扫描弹窗 -->
    <el-dialog v-model="showScanDialog" title="扫描局域网设备" width="720px">

        <!-- 扫描中状态 -->
        <div v-if="scanningLAN" class="text-center py-12">
          <div class="inline-block animate-spin rounded-full h-8 w-8 border-4 border-primary-500 border-t-transparent mb-3"></div>
          <p class="text-slate-500 text-sm">正在扫描局域网，请稍候...</p>
          <p class="text-xs text-slate-400 mt-1">WS-Discovery + 端口扫描，约 5-15 秒</p>
        </div>

        <!-- 扫描结果 -->
        <div v-else>
          <el-alert v-if="scanError" :title="scanError" type="error" :closable="false" class="mb-3" />

          <div v-if="scannedDevices.length > 0" class="border border-slate-200 rounded-md overflow-hidden">
            <!-- 全选 -->
            <div class="px-3 py-2 bg-slate-50 border-b border-slate-200 text-xs font-medium text-slate-600 flex items-center gap-2">
              <el-checkbox
                :model-value="allDevicesSelected"
                :indeterminate="selectedDevices.length > 0 && !allDevicesSelected"
                @change="toggleAllDevices"
              >
                全选 ({{ selectedDevices.length }}/{{ scannedDevices.length }})
              </el-checkbox>
              <span class="ml-auto text-slate-400">支持品牌: 海康威视 · 宇视 · 通用 ONVIF</span>
            </div>

            <!-- 设备列表 -->
            <el-checkbox-group v-model="selectedDevices" class="block">
              <el-checkbox
                v-for="dev in scannedDevices"
                :key="dev.ip"
                :value="dev.ip"
                class="flex w-full items-center px-3 py-2.5 border-b border-slate-100 last:border-b-0"
              >
                <span class="flex min-w-0 flex-1 items-center gap-3">
                  <!-- 品牌颜色标记 -->
                  <span
                    class="w-2.5 h-2.5 rounded-full flex-shrink-0"
                    :class="dev.brand === 'hikvision' ? 'bg-red-500' : dev.brand === 'uniview' ? 'bg-blue-500' : 'bg-slate-300'"
                  ></span>
                  <!-- 设备信息 -->
                  <span class="flex-1 min-w-0">
                    <span class="flex items-center gap-2">
                      <span class="font-mono text-sm font-medium text-slate-800">{{ dev.ip }}</span>
                      <span class="px-1.5 py-0.5 bg-slate-100 text-slate-600 rounded text-[10px] font-medium">
                        {{ brandLabel(dev.brand) }}
                      </span>
                      <span v-if="dev.device_type" class="px-1.5 py-0.5 rounded text-[10px] font-medium"
                        :class="dev.device_type === 'nvr' ? 'bg-purple-100 text-purple-700' : 'bg-blue-100 text-blue-700'">
                        {{ deviceTypeLabel(dev.device_type) }}
                      </span>
                    </span>
                    <span class="block text-xs text-slate-500 mt-0.5">
                      <span v-if="dev.manufacturer">{{ dev.manufacturer }}</span>
                      <span v-if="dev.model"> · {{ dev.model }}</span>
                      <span v-if="dev.channels > 0"> · {{ dev.channels }} 通道</span>
                      <span v-if="dev.rtsp_enabled"> · RTSP 已启用</span>
                    </span>
                  </span>
                </span>
              </el-checkbox>
            </el-checkbox-group>
          </div>

          <!-- 底部操作 -->
          <div class="flex items-center justify-between mt-4">
            <el-button
              plain
              size="small"
              @click="handleScanLAN"
            >
              <AppIcon name="refresh" class="w-3.5 h-3.5" />
              <span>重新扫描</span>
            </el-button>
            <div class="flex gap-2">
              <el-button
                plain
                @click="showScanDialog = false"
              >
                取消
              </el-button>
              <el-button
                type="primary"
                @click="handleBatchAdd"
                :disabled="selectedDevices.length === 0"
                :loading="submitting"
              >
                添加 {{ selectedDevices.length }} 个设备
              </el-button>
            </div>
          </div>

          <p class="text-xs text-slate-400 mt-3 flex items-start gap-1.5">
            <AppIcon name="info" class="w-3.5 h-3.5 mt-px flex-shrink-0" />
            <span>设备添加后需在列表中编辑用户名和密码，才能正常拉流。</span>
          </p>
        </div>
    </el-dialog>

    <!-- 网络配置弹窗 -->
    <el-dialog v-model="showNetworkConfigDialog" title="网络配置" width="480px">
        <p class="text-xs text-slate-500 mb-3">修改 {{ networkConfigCamera?.name }} 的 IP 地址。修改后设备会重启，需使用新 IP 重新连接。</p>

        <div class="space-y-3">
          <el-checkbox v-model="networkForm.dhcp">使用 DHCP 自动获取 IP</el-checkbox>

          <div v-if="!networkForm.dhcp" class="space-y-2">
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">IP 地址</label>
              <el-input v-model="networkForm.ip" placeholder="192.168.1.100" />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">子网掩码</label>
              <el-input v-model="networkForm.mask" placeholder="255.255.255.0" />
            </div>
            <div>
              <label class="block text-sm font-medium text-slate-700 mb-1">网关</label>
              <el-input v-model="networkForm.gateway" placeholder="192.168.1.1（留空自动推断）" />
            </div>
          </div>

          <div class="bg-amber-50 border border-amber-200 rounded px-3 py-2 text-xs text-amber-700 flex items-start gap-1.5">
            <AppIcon name="warning" class="w-3.5 h-3.5 mt-px flex-shrink-0" />
            <span>修改 IP 后，当前连接会断开。请确保新 IP 与服务器在同一网段。</span>
          </div>
        </div>

        <div class="flex justify-end gap-2 mt-4">
          <el-button plain @click="showNetworkConfigDialog = false">取消</el-button>
          <el-button type="primary" :loading="settingNetwork" @click="handleSubmitNetwork">应用</el-button>
        </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import AppIcon from '../components/AppIcon.vue'
import {
  listCameras, createCamera, updateCamera, deleteCamera, syncCameraTime, listLocalCameras,
  testCameraConnection, testCameraConnectionByIP, discoverNVRChannels, scanNetwork, setCameraCodec,
  setCameraNetwork,
} from '../api'

const cameras = ref([])
const loading = ref(true)
const cameraDialogOpen = computed({
  get: () => showAddDialog.value || Boolean(editingCamera.value),
  set: (open) => {
    if (!open) closeDialog()
  },
})
const showAddDialog = ref(false)
const editingCamera = ref(null)
const syncingId = ref(null)
const testingId = ref(null)
const testingByIP = ref(false)
const testResult = ref(null)
const testInfoModal = ref(null)
const testInfoDialogOpen = ref(false)
const submitting = ref(false)
const submitError = ref('')
const submitSuccess = ref('')
const scanningLocal = ref(false)
const localCameraList = ref([])
const discovering = ref(false)
const discoveredChannels = ref([])
const selectedChannels = ref([])
const showScanDialog = ref(false)
const scanningLAN = ref(false)
const scannedDevices = ref([])
const selectedDevices = ref([])
const scanError = ref('')
const showNetworkConfigDialog = ref(false)
const networkConfigCamera = ref(null)
const networkForm = ref({ dhcp: false, ip: '', mask: '255.255.255.0', gateway: '', dns: '' })
const settingNetwork = ref(false)

// 全选状态
const allChannelsSelected = computed(() =>
  discoveredChannels.value.length > 0 &&
  selectedChannels.value.length === discoveredChannels.value.length
)

const toggleAllChannels = (checked) => {
  if (checked) {
    selectedChannels.value = discoveredChannels.value.map(ch => ch.channel)
  } else {
    selectedChannels.value = []
  }
}

const defaultForm = () => ({
  name: '',
  ip: '',
  port: 554,
  brand: 'custom',
  device_type: 'ipc',
  nvr_channel: 0,
  preferred_codec: 'auto',
  stream_type: 'main',
  username: '',
  password: '',
  auto_tune_enabled: true,
  access_protocol: 'rtsp',
  device_id: '',
  channel_id: '',
  transport: 'UDP',
  local_index: -1,
  local_vid: '',
  local_pid: '',
  local_name: '',
})
const form = ref(defaultForm())

const loadCameras = async () => {
  loading.value = true
  try {
    cameras.value = await listCameras()
  } finally {
    loading.value = false
  }
}

onMounted(loadCameras)

const brandLabel = (b) => {
  const map = { hikvision: '海康威视', uniview: '宇视', custom: '自定义' }
  return map[b] || b
}
const deviceTypeLabel = (t) => {
  const map = { ipc: 'IPC', nvr: 'NVR', dvr: 'DVR', encoder: '编码器' }
  return map[t] || t
}
const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const handleEdit = (cam) => {
  editingCamera.value = cam
  form.value = { ...defaultForm(), ...cam }
  showAddDialog.value = false
  testResult.value = null
  discoveredChannels.value = []
  selectedChannels.value = []
}

const closeDialog = () => {
  showAddDialog.value = false
  editingCamera.value = null
  form.value = defaultForm()
  submitError.value = ''
  submitSuccess.value = ''
  testResult.value = null
  discoveredChannels.value = []
  selectedChannels.value = []
}

// 局域网扫描
const handleScanLAN = async () => {
  showScanDialog.value = true
  scanningLAN.value = true
  scannedDevices.value = []
  selectedDevices.value = []
  scanError.value = ''
  try {
    const devices = await scanNetwork({ subnet: 'auto' })
    scannedDevices.value = devices || []
    if (devices.length === 0) {
      scanError.value = '未发现设备。请确保设备与服务器在同一网段。'
    }
  } catch (err) {
    scanError.value = '扫描失败: ' + (err.response?.data?.message || err.message)
  } finally {
    scanningLAN.value = false
  }
}

const allDevicesSelected = computed(() =>
  scannedDevices.value.length > 0 &&
  selectedDevices.value.length === scannedDevices.value.length
)

const toggleAllDevices = (checked) => {
  if (checked) {
    selectedDevices.value = scannedDevices.value.map(d => d.ip)
  } else {
    selectedDevices.value = []
  }
}

// 从扫描结果批量添加设备
const handleBatchAdd = async () => {
  if (selectedDevices.value.length === 0) return
  submitting.value = true
  submitError.value = ''
  submitSuccess.value = ''
  let added = 0
  try {
    for (const ip of selectedDevices.value) {
      const dev = scannedDevices.value.find(d => d.ip === ip)
      if (!dev) continue
      const data = {
        name: `${dev.manufacturer || 'Camera'}-${dev.ip.split('.').pop()}`,
        ip: dev.ip,
        port: 554,
        brand: dev.brand || 'custom',
        device_type: dev.device_type || 'ipc',
        nvr_channel: 0,
        rtsp_url: dev.rtsp_url || '',
        username: '',
        password: '',
        access_protocol: 'rtsp',
        auto_tune_enabled: true,
      }
      await createCamera(data)
      added++
    }
    showScanDialog.value = false
    alert(`成功添加 ${added} 个设备。请在列表中编辑凭据。`)
    loadCameras()
  } catch (err) {
    submitError.value = '部分设备添加失败: ' + (err.response?.data?.message || err.message)
  } finally {
    submitting.value = false
  }
}

// 测试连接（通过 IP）
const handleTestByIP = async () => {
  testingByIP.value = true
  testResult.value = null
  try {
    const info = await testCameraConnectionByIP({
      ip: form.value.ip,
      username: form.value.username,
      password: form.value.password,
    })
    const label = info.manufacturer ? `${info.manufacturer} ${info.model}` : '连接成功'
    testResult.value = { ok: true, message: label + (info.permission_note ? '（权限受限）' : '') }
    testInfoModal.value = info
    testInfoDialogOpen.value = true
  } catch (err) {
    testResult.value = { ok: false, message: '连接失败: ' + (err.response?.data?.message || err.message) }
  } finally {
    testingByIP.value = false
  }
}

// 测试连接（通过已保存的摄像头 ID）
const handleTest = async (cam) => {
  testingId.value = cam.id
  try {
    const info = await testCameraConnection(cam.id)
    testInfoModal.value = info
    testInfoDialogOpen.value = true
    const label = info.manufacturer ? `${info.manufacturer} ${info.model}` : '设备'
    alert(`${label} 连接成功` + (info.permission_note ? '\n注意：' + info.permission_note : ''))
  } catch (err) {
    alert('连接失败：' + (err.response?.data?.message || err.message))
  } finally {
    testingId.value = null
  }
}

const closeTestInfoDialog = () => {
  testInfoDialogOpen.value = false
}

const clearTestInfoDialog = () => {
  testInfoModal.value = null
}

// 扫描 NVR 通道
const handleDiscoverChannels = async () => {
  discovering.value = true
  discoveredChannels.value = []
  selectedChannels.value = []
  try {
    const channels = await discoverNVRChannels({
      ip: form.value.ip,
      username: form.value.username,
      password: form.value.password,
    })
    discoveredChannels.value = channels || []
    if (channels.length === 0) {
      alert('未发现可用通道。请确认 IP、用户名和密码正确。')
    }
  } catch (err) {
    alert('扫描失败: ' + (err.response?.data?.message || err.message))
  } finally {
    discovering.value = false
  }
}

const handleSubmit = async () => {
  submitting.value = true
  submitError.value = ''
  submitSuccess.value = ''
  try {
    const isNVR = form.value.device_type === 'nvr' || form.value.device_type === 'dvr'

    // NVR 模式 + 选中了多个通道: 批量处理
    if (isNVR && selectedChannels.value.length > 0) {
      // 第一个通道更新到当前摄像头（或新建）
      const firstCh = selectedChannels.value[0]
      const firstChInfo = discoveredChannels.value.find(c => c.channel === firstCh)

      if (editingCamera.value) {
        // 更新现有摄像头为第一个选中通道
        const updateData = {
          ...form.value,
          nvr_channel: firstCh,
          rtsp_url: firstChInfo?.rtsp_url || null,  // null 让后端重建或设为发现的地址
        }
        await updateCamera(editingCamera.value.id, updateData)
      } else {
        // 新建第一个摄像头
        const createData = {
          ...form.value,
          nvr_channel: firstCh,
          name: `${form.value.name}-CH${firstCh}`,
          rtsp_url: firstChInfo?.rtsp_url || '',
        }
        await createCamera(createData)
      }

      // 剩余通道新建
      let added = 1
      for (let i = 1; i < selectedChannels.value.length; i++) {
        const ch = selectedChannels.value[i]
        const chInfo = discoveredChannels.value.find(c => c.channel === ch)
        const data = {
          ...form.value,
          nvr_channel: ch,
          name: `${form.value.name}-CH${ch}`,
          rtsp_url: chInfo?.rtsp_url || '',
        }
        await createCamera(data)
        added++
      }

      submitSuccess.value = `${editingCamera.value ? '更新并' : ''}成功添加 ${added} 个通道`
      setTimeout(() => {
        closeDialog()
        loadCameras()
      }, 1000)
      submitting.value = false
      return
    }

    if (isNVR && !form.value.nvr_channel) {
      submitError.value = 'NVR/DVR 设备需要指定通道号或扫描选择通道'
      submitting.value = false
      return
    }
    if (form.value.device_type === 'ipc') {
      form.value.nvr_channel = 0
    }

    if (editingCamera.value) {
      // 编辑模式：如果设备类型/通道/品牌等改了，让后端自动重建 RTSP URL
      const updateData = { ...form.value }
      if (isNVR || form.value.device_type === 'ipc') {
        updateData.rtsp_url = null  // 让后端根据当前配置重建
      }
      await updateCamera(editingCamera.value.id, updateData)
      submitSuccess.value = '更新成功'
    } else {
      await createCamera(form.value)
      submitSuccess.value = '添加成功，正在后台自动调优...'
    }
    setTimeout(() => {
      closeDialog()
      loadCameras()
    }, 1000)
  } catch (err) {
    submitError.value = err.response?.data?.message || '操作失败'
  } finally {
    submitting.value = false
  }
}

const handleDelete = async (cam) => {
  if (!confirm(`确定要删除摄像头 "${cam.name}" 吗？`)) return
  try {
    await deleteCamera(cam.id)
    loadCameras()
  } catch (err) {
    alert('删除失败: ' + (err.response?.data?.message || err.message))
  }
}

// 设置编码格式
const handleSetCodec = async (cam, codec) => {
  try {
    await setCameraCodec(cam.id, codec)
    alert(`"${cam.name}" 编码格式已设置为 ${codec.toUpperCase()}`)
    loadCameras()
  } catch (err) {
    alert('设置编码失败: ' + (err.response?.data?.message || err.message))
  }
}

// 网络配置弹窗
const showNetworkDialog = (cam) => {
  networkConfigCamera.value = cam
  networkForm.value = {
    dhcp: false,
    ip: cam.ip || '',
    mask: '255.255.255.0',
    gateway: '',
    dns: '',
  }
  showNetworkConfigDialog.value = true
}

const handleSubmitNetwork = async () => {
  if (!networkConfigCamera.value) return
  settingNetwork.value = true
  try {
    const result = await setCameraNetwork(networkConfigCamera.value.id, networkForm.value)
    showNetworkConfigDialog.value = false
    alert(result.message || '网络配置已设置，设备正在重启')
  } catch (err) {
    alert('设置网络失败: ' + (err.response?.data?.message || err.message))
  } finally {
    settingNetwork.value = false
  }
}

const handleSyncTime = async (cam) => {
  syncingId.value = cam.id
  try {
    await syncCameraTime(cam.id)
    alert(`"${cam.name}" 时间同步成功`)
    loadCameras()
  } catch (err) {
    alert('同步失败: ' + (err.response?.data?.message || err.message))
  } finally {
    syncingId.value = null
  }
}

const scanLocalCameras = async () => {
  scanningLocal.value = true
  localCameraList.value = []
  try {
    localCameraList.value = await listLocalCameras()
  } catch (err) {
    alert('扫描失败: ' + (err.response?.data?.message || err.message))
  } finally {
    scanningLocal.value = false
  }
}

const selectLocalCamera = (cam) => {
  form.value.local_index = cam.index
  form.value.local_name = cam.name
  if (cam.path) form.value.ip = cam.path
  if (!form.value.name) form.value.name = cam.name
}
</script>
