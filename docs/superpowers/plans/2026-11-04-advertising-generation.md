# C：广告镜头、TTS与成片交付 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task（用户已选择Lead内联实现，最后一次独立整分支评审；不派实现子代理）。Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将B的不可变脚本接到逐镜头视频/TTS生成、人工采用、既有时间线和可下载MP4。
**Architecture:** 广告层只保存制作、获批提交、组装和渲染幂等记录；复用Task/账务/Resource/CanvasProject/FFmpeg。准备与执行分离，任务和订单事务准入，完成时事务登记候选引用。
**Tech Stack:** Go/Gin/GORM/SQLite/PostgreSQL；React/TypeScript/Bun/AntD/TanStack Query；现有ffprobe/ffmpeg。无新生产依赖。
**Spec:** [已批准C规格](../specs/2026-11-04-advertising-generation-design.md)。基线cea76a66，计划待用户审阅，尚未开始实施。

## Global Constraints

- 中文、单商品、3–5镜头、15–30秒、1080×1920；选定确认版本后不自动跟随最新版本。B草稿/确认内容不得改写。
- 逐镜头video/audio单产物；TTS速度1，空旁白不生成；默认统一音色但不自动重生成已有候选。
- 旁白超镜头时长、视频裁剪区间不足、时长未知均阻止组装；不截旁白、不变速、不补黑/末帧。
- 每次生成报价有效期5分钟，显式批准；不自动重发新生成，不自动采用。收费上限必须由账务实际执行，不支持的计价模式拒绝报价。
- 只用服务端受管模型，客户端不提交密钥、任意provider协议或费用；参考图按规格选取，不静默忽略不支持的引用。
- 背景音乐1个，默认音量0.15、范围0–1，短音乐不循环；原视频静音，旁白音量1；onScreenText按镜头区间显示。
- 严格JSON≤256KiB、分页默认20/最大100、UUID操作键、模型/音色标识≤120字；当前用户scope隔离与CAS。
- 沿原生版本化GORM迁移。当前schema79，新增80；实施前若版本前移，只顺延新迁移，不改旧checksum。不引入Atlas。
- 真实模型验收另行指定模型和预算；规格/计划批准不授权付费调用。PG/Redis/OSS无环境时列为未覆盖。
- 继续当前隔离分支；不推送/合并、不启动生产服务、不接现有业务数据。浏览器验收只用此前授权的隔离本地环境，结束关闭。

## Review Focus

以下五项已在对应任务加入断言，最终独立评审仍重点检查：
1. 首次提交成功但响应丢失、其后过期/涨价再重放：必须返回原Task，零新增订单（Task3）。
2. 音频按实际秒数结算高于估计：不能超过确认额度，未知结果不得变成自动退款后重复生成（Task2/3）。
3. 完成/回写/删除相互竞争：不复活广告、不留下无引用候选、不删除独立媒体（Task4/6）。
4. 新版本、旧报价、另一镜头候选混用及页面账号切换：拒绝越界且不丢本地输入（Task3/4/7）。
5. 新组装后手改画布，随后旧请求重放：不覆盖手改画布，不把手改导出标成原组装（Task5/6）。

## 文件与接口分工

新增backend/internal/model/models_advertising_generation.go保存四表；repository_advertising_generation.go保存制作CAS/提交/组装/尝试查询。业务按advertising_production.go（DTO/设置）、advertising_submission.go（报价执行）、advertising_assembly.go（组装）、advertising_generation_test.go（合同）、advertising_assembly_test.go（真实媒体）划分。不拆单实现接口层。

复用并定点修改creation.go/task_creation.go/service.go、storage_quota.go、task_timeline.go、task_render.go、model_router_attempts.go、repository_tasks.go、finance.go/finance_billing.go、repository_advertising.go、resource_reference.go及resource_cleanup.go。任何新增修改路径须先grep全部调用方并记录到执行ledger，禁止无关重构。

HTTP增加handler/advertising_generation.go与对应测试，service/aliases_advertising.go只做导出；openapi.yaml和错误码同步。前端增加services/api/advertising-generation.ts、pages/ad-projects/advertising-production.tsx、advertising-shot-generation.tsx，沿用现有assets组件和advertising.css。回归保留在web/test/advertising-generation-*.test.*及web/scripts/advertising-generation-chrome-e2e.mjs。

## 共享DTO（由Task1/3定义，后续不可另造同名异形类型）

- AdvertisingProductionSettings：ttsModelId、voiceId、selections(map[shotId]AdvertisingSelection)、musicResourceId、musicVolume。AdvertisingSelection={videoSubmissionId,audioSubmissionId,videoStartMs}，全部引用同production；settings未提供musicVolume时默认0.15，显式0保留。
- AdvertisingProductionDetail：production(AdvertisingProductionSummary)、settings、confirmedSnapshot(AdvertisingRevisionDetail)；summary字段id/projectId/confirmedRevisionId/revision/createdAt/updatedAt，不暴露settingsJson。AdvertisingPage[T]复用B分页结构，若Go尚无泛型类型则本任务建立一个，不改已有HTTP信封。
- AdvertisingSubmissionRequest：clientOperationId、shotId、kind(video/audio)、modelId、voiceId、format。video时voiceId/format拒绝；audio格式限受管能力支持的mp3/wav，默认mp3。模板/输出分辨率/供应商时长由服务器按能力确定并在报价返回。
- AdvertisingSubmissionDetail：id、productionId、shotId、kind、requestHash、publicRequest、quote(CreationQuote复用结构)、task(可空公开Task DTO)。publicRequest含最终提示词、资源ID、实际所选时长/尺寸/音色，无URL/密钥。
- AdvertisingAssemblyDetail：assembly、canvasId、timeline(标准v2)、attempts分页独立查询；AdvertisingRenderAttemptDetail为attempt及公开Task。
- 错误reason统一前缀advertising_：production_conflict、quote_expired、quote_changed、submission_mismatch、candidate_unavailable、media_duration_invalid、approval_required；已有invalid_document/resource_unavailable/active_tasks复用。

### Task 1: 制作持久化、CAS与schema80

**Files:** 新增model/models_advertising_generation.go、repository/repository_advertising_generation.go、app/advertising_production.go、database/advertising_generation_migration_test.go、app/advertising_generation_test.go；修改database/migrations.go、schema.go、model/models_advertising.go、cmd/migrate-sqlite-postgres/main.go及main_test.go、docs/content/docs/backend/backend-database.mdx。
**Interfaces:** CreateAdvertisingProduction(userID,projectID,confirmedRevisionID string)(AdvertisingProductionDetail,error)；GetAdvertisingProduction(userID,projectID,productionID string)(AdvertisingProductionDetail,error)；SaveAdvertisingProductionSettings(userID,projectID,productionID string, expectedRevision int64, settings AdvertisingProductionSettings)(AdvertisingProductionDetail,error)。repository提供AdvertisingProductionTransaction(userID,projectID,productionID string, fn func(*Repository)error)error，沿B锁策略，不在持锁事务中做网络/ffprobe。

- [ ] RED：TestAdvertisingGenerationMigration测试四表/外键/唯一键、79→80、重跑、SQLite拷贝；TestAdvertisingProductionCAS测试同确认唯一制作、跨用户404、非法shotId/字段拒绝、CAS输家409、音量0不变默认值。执行`cd backend && go test ./internal/database ./cmd/migrate-sqlite-postgres ./internal/app -run "TestAdvertisingGenerationMigration|TestAdvertisingProductionCAS" -count=1`，预期缺模型/方法而失败。
- [ ] 实现四模型，字段按规格§5；ID用现有newID，taskId/canvasId使用nullable列而非空串FK。production与确认版本组合归属校验；taskId唯一、引用RESTRICT。assembly独立revision来源，attempt唯一(assemblyId,clientOperationId)。
- [ ] **必要的已有字段调整：** AdvertisingResourceReference.ScopeKey当前size36，production:<32hex>最长43，schema80将其改为size64，保留现有scope值/主键/FK。测试写入最长scope及升级前draft/确认引用仍在，PG检查真实列宽，不能让SQLite宽松类型掩盖溢出。
- [ ] 实现创建/读取/设置的归属、严格shotId、范围校验与CAS；本任务空采用设置可保存，非空候选需真实有效提交，Task4补完整采用路径；不得临时允许任意submissionId。
- [ ] GREEN：同RED命令通过；CANVAS_TEST_POSTGRES_DSN随机schema运行可选迁移合同；无DSN明确skip。提交`feat(advertising): 广告制作 - 增加制作与提交持久化`。

### Task 2: 共用无副作用任务准备与可执行费用上限

**Files:** app/creation.go、task_creation.go、service.go、task_billing.go；repository/finance_billing.go；新增app/task_preparation.go、app/advertising_billing_test.go。
**Interfaces:** prepareManagedTask(userID string, req CreateTaskRequest)(*model.Task,*model.BillingOrder,string,error)只负责复用现有CreateTask准备路径及价格签名，不执行画布方案校验；prepareCreationTask保留全部原有画布限制后调用它。AdvertisingSubmission仅调用新共用函数；CreateTaskRequest内新增非JSON内部广告授权标记，不可由HTTP伪造。

- [ ] RED：TestAdvertisingManagedPreparation对canvas_video/canvas_audio准备，断言tasks/orders/ledger新增均0、私有渠道/任意协议被拒绝；TestAdvertisingBillingCeiling断言音频实际用量使应付超过批准上限时，用户最终扣款≤ChargeLimitMicrocredits；价格签名覆盖音色/时长/价格档/倍率。运行`go test ./internal/app ./internal/repository -run "TestAdvertisingManagedPreparation|TestAdvertisingBillingCeiling" -count=1`，先观察失败。
- [ ] 提取现有creationPrepare机制的可复用最小部分；不得简单放开CreationRun的audio，因为其画布审批不属于C。检查并保持cloud_agent_runtime_tools.go等准备调用方原行为。共用类型若改名，更新所有caller/tests。
- [ ] 复用BillingOrder.ChargeLimitMicrocredits及结算限制，广告审批设置实际授权上限。逐个覆盖受支持计价模式；无法安全封顶的模式返回400不可报价，不另造账务，不悄悄追加扣款。实际媒体探测未知进入既有uncertain/review规则。
- [ ] GREEN：上述检查及`go test ./internal/app ./internal/repository -run "TestCreation|TestCloudAgent.*(Media|Image)|TestTaskExecutionContract|Test.*Billing"`通过；现有环境缺runtime依赖须列为未覆盖，不假称全包通过。提交`refactor(tasks): 广告生成 - 复用任务准备与费用上限`。

### Task 3: 视频/TTS报价、显式执行及幂等准入

**Files:** 新增app/advertising_submission.go；扩展app/advertising_generation_test.go、repository_advertising_generation.go；修改service.go/RetryTask、task_creation.go、repository/finance.go、app/model_router_attempts.go及实际调用provider retry决策处。
**Interfaces:** PrepareAdvertisingSubmission(userID,projectID,productionID string, req AdvertisingSubmissionRequest)(AdvertisingSubmissionDetail,error)；ExecuteAdvertisingSubmission(userID,projectID,productionID,submissionID,quoteHash string)(AdvertisingSubmissionDetail,error)；ListAdvertisingSubmissions(userID,projectID,productionID string,page,pageSize int)(AdvertisingPage[AdvertisingSubmissionDetail],error)。

- [ ] RED：TestAdvertisingSubmissionLifecycle：准备零任务、5min边界过期、同operation不同正文409、两并发执行仅1Task/1预授权、成功后过期/涨价重放仍同Task、保存taskId失败整事务回滚、另用户/镜头/production拒绝。TestAdvertisingApprovalBypass：通用CreateTask/RetryTask无内部授权拒绝广告任务；请求中伪造metadata无效。先跑`go test ./internal/app -run "TestAdvertisingSubmission|TestAdvertisingApproval" -count=1`观察RED。
- [ ] 实现固定版本提示模板（版本1），冻结商品信息+visual；原顺序图片引用，没有显式引用取首商品图；引用视频/音频明确400。按受管目录选≥镜头时长最小视频档位、9:16支持、图数量及输出大小；不支持能力拒绝，不猜provider参数。TTS冻结voiceover，语速1，空白拒绝。报价显示最终选择，不保留临时签名URL。
- [ ] quoteHash覆盖requestHash/价签/金额/计价数量；准备持久化规范请求。执行先重放查询，再校验过期/价签/当前权限/配额，事务内写Task、订单与关联。沿B删除配对锁，所有会访问资源的锁顺序与删除一致；SQL错误不得转成功空任务。
- [ ] 通过真实submission关系识别广告任务，不依赖未验证metadata。封住通用重试；广告新生成的自动重试/failover关闭，轮询/已知provider请求恢复保留。用假provider计数测试“首次响应丢失/5xx/worker重启”不出现第二次新生成；供应商不支持恢复时标uncertain，等待人工而非重发。
- [ ] GREEN：上面命令+`go test ./internal/repository -run "Test.*Task|Test.*Billing"`；PG覆盖删除提交先后两种顺序，无DSN只skip。提交`feat(advertising): 广告生成 - 实现报价批准与幂等任务提交`。

### Task 4: 候选完成登记、采用与生命周期保护

**Files:** app/storage_quota.go、advertising_production.go、repository/repository_tasks.go、repository_advertising_generation.go、repository_advertising.go、resource_reference.go、resource_cleanup.go；新增app/advertising_candidate_test.go。
**Interfaces:** registerAdvertisingTaskOutput(repo *repository.Repository, task model.Task)error，加入已有SaveTaskCompletionWithRegistration注册链；不替换原RegisterTaskOutputFromTask/registerRecoveredMediaAssets。SaveAdvertisingProductionSettings完成候选采用验证；getAdvertisingCandidateResource(repo *repository.Repository,userID,productionID,shotID,kind,submissionID string)(*model.Resource,error)供Task5复用。

- [ ] RED：TestAdvertisingCandidateProtection验证后台成功后无需GET就存在candidate引用；失败/取消/未落库产物不可采用；跨版本/镜头/类型拒绝；换采用项旧候选仍保护；完成注册失败终态回滚。TestAdvertisingProductionDeletion验证删除活动任务409、终态广告删除解除自身引用但保留媒体与账务、等待完成不复活已删项目。
- [ ] 复用真实Task结果Resource解析并强校验ready/MIME/归属/单一产物。在完成事务写candidate引用及既有Asset登记，迟到完成遵守lease fencing。媒体修复/恢复成功也走同注册入口。
- [ ] production音乐引用随CAS事务更新；reference查询报告准确scope来源。通过submission.taskId/attempt.taskId的RESTRICT FK保护关联Task，审查通用历史清理/账号删除路径：普通清理跳过或返回占用，不先删结果后触发FK。全广告删除按attempt→assembly→submission→production→B内容顺序清理，Canvas只解绑。
- [ ] GREEN：`go test ./internal/app ./internal/repository -run "TestAdvertisingCandidate|TestAdvertisingProductionDeletion|TestAdvertisingResource|Test.*Lease" -count=1`；补真实bind/delete及完成/delete竞争PG可选检查。提交`feat(advertising): 广告候选 - 保护产物引用与采用状态`。

### Task 5: 校验真实时长并组装标准时间线与新画布

**Files:** 新增app/advertising_assembly.go、app/advertising_assembly_test.go；扩展repository_advertising_generation.go；复用media_probe.go、timeline_render_validation.go、canvas_mutation_receipts.go及现有CanvasProject创建/Asset登记方法。
**Interfaces:** CreateAdvertisingAssembly(userID,projectID,productionID string, expectedRevision int64, clientOperationID,fit string)(AdvertisingAssemblyDetail,error)；GetAdvertisingAssembly(userID,projectID,productionID,assemblyID string)(AdvertisingAssemblyDetail,error)。ListAdvertisingAssemblies(userID,projectID,productionID string,page,pageSize int)(AdvertisingPage[AdvertisingAssemblyDetail],error)。内部buildAdvertisingTimeline(snapshot AdvertisingRevisionDetail,settings AdvertisingProductionSettings,media map[string]AdvertisingProbedMedia,fit string)(map[string]any,error)，AdvertisingProbedMedia={Resource model.Resource,DurationMs int64,Width,Height int}；输出严格匹配现有TimelineProject v2。

- [ ] RED：TestAdvertisingAssemblyDuration：镜头5000ms/音频5001ms拒绝，5000ms通过，未知0拒绝；视频起点1000+镜头5000而源5999拒绝；音量0保留、短音乐不循环、空旁白无audio轨、有旁白未采用拒绝。TestAdvertisingAssemblyReplay：重复key同来源返回同canvas，key不同输入409；手改原画布后重放不覆盖；新组装新canvas。
- [ ] 用probeMediaFile及现有受限资源读取/临时文件检查真实流、尺寸、时长。探测在事务外，事务内重新核对production revision及资源身份/可读状态后提交；不信任Resource durationMs作为组装唯一证据。缺ffprobe明确不可组装。
- [ ] 构造连续video轨（volume0）、逐镜头audio轨（volume1、实际时长）、可选music轨（默认0.15）、onScreenText轨及Logo图层，输出1080×1920/30fps，默认pad、可选crop。视频trimStart按采用值，clip长度等镜头时长。UI对应预览需遵守同区间。
- [ ] 原子写assembly.timelineJson、selectionHash、assembly资源引用与新CanvasProject。用已有资源→Asset登记供时间线恢复，不复制字节；画布payload必须由真实CanvasProject恢复逻辑接受，集成测试用前端timeline snapshot工具读取，不只Go map相等。
- [ ] GREEN：`go test ./internal/app -run "TestAdvertisingAssembly" -count=1`，含真实ffmpeg短音视频夹具；无二进制明确skip，纯逻辑仍必跑。提交`feat(advertising): 广告组装 - 生成受约束时间线与独立画布`。

### Task 6: 幂等渲染尝试与成片来源

**Files:** app/advertising_assembly.go、task_timeline.go、task_render.go、repository_advertising_generation.go；扩展app/advertising_assembly_test.go、task_render_test.go。
**Interfaces:** CreateAdvertisingRender(userID,projectID,productionID,assemblyID,clientOperationID string)(AdvertisingRenderAttemptDetail,error)；ListAdvertisingRenderAttempts(userID,projectID,productionID,assemblyID string,page,pageSize int)(AdvertisingPage[AdvertisingRenderAttemptDetail],error)。从CreateTimelineRenderTask抽出必要的内部准备/事务入队步骤供原入口和本入口复用，保持原功能开关/配额/归属，不在事务内启动worker网络。

- [ ] RED：TestAdvertisingRenderReplay：并发同key一Task/一attempt，后续新key失败尝试后旧key仍返回最初Task；关联写失败任务不提交。TestAdvertisingRenderOutput：使用有明显画面变化的3镜头+旁白+音乐+文字+Logo夹具，输出15000ms±34ms、1080×1920、可解码音视频，资源丢失/被删项目拒绝导出。
- [ ] 渲染仅取不可变assembly.timelineJson，写attempt及任务同事务。原组装成功产物保存assembly/attempt引用；禁止后来导出覆盖旧Task结果。总时长、音轨、输出资源可读取后才“成片可下载”；任务排队或只有Resource记录均不可声称完成。
- [ ] 保留手改画布导出既有路径，UI区分它与原组装重放；其普通画布Task不得伪造广告attempt。任务中心可取消，取消不删除成功资源。
- [ ] GREEN：`go test ./internal/app -run "TestAdvertisingRender|TestTimelineRender|TestCreateTimelineRender" -count=1`。对本地MP4做ffprobe及可解码帧/静音区间检查，不用黑片替代；无工具列为环境阻塞。提交`feat(advertising): 广告成片 - 关联幂等渲染与产物来源`。

### Task 7: 鉴权API及真实广告制作界面

**Files:** 新增handler/advertising_generation.go、handler/advertising_generation_test.go、web/src/services/api/advertising-generation.ts、web/src/pages/ad-projects/advertising-production.tsx、advertising-shot-generation.tsx、web/test/advertising-generation-api.test.ts、advertising-generation-interaction.test.tsx；修改handler/api.go/openapi.yaml、service/aliases_advertising.go、kernel/error_codes.go/app/error_codes.go、web/services/api/error-codes.ts、ad-projects/detail.tsx/advertising.css。
**Interfaces:** DTO沿共享定义；API导出create/get/saveAdvertisingProduction、prepare/execute/listAdvertisingSubmissions、create/get/listAdvertisingAssemblies、create/listAdvertisingRenders，参数/返回一对一对应服务签名，http自动解包；所有GET支持AbortSignal。端点完整展开规格§9，GET列表统一items/page/pageSize/total。

- [ ] RED：HTTP每条路由未登录拒绝、跨用户404、短剧关闭仍可用、未知/重复JSON键/oversize/非法revision拒绝；quote及production不同标识不可串用。API adapter测试验证method/path/body/query/错误reason。
- [ ] React真实DOM测试：未确认无制作、切换版本显式、报价过期重报价、提交响应丢失恢复同Task、执行中刷新恢复、未采用禁止组装、超时提示镜头号/实际与目标ms；失败保留选择、CAS对照、切账号/项目的迟到响应不污染；网络写失败不自动换操作键。
- [ ] 实现受管模型/音色控件，优先复用provider-catalog及现有配置解析，合法选项不足明确空态。按钮分开“获取报价”“确认费用并生成”“采用”“组装”“导出原组装”“打开时间线”；禁止无确认启动网络生成。default voice变更不伪装已有候选统一。
- [ ] 视频/音频使用原生播放控件预览；拟用区间显示起止、真实媒体时长；字幕/Logo组装预览复用现有timeline组件。音乐从既有资源选择/上传绑定；对pendingRemoteUpload明确拒绝。
- [ ] 成功组装显示来源与新画布入口，分页候选/尝试，后端未返回succeeded且resource可读前不出现下载成功。确认费用使用AppModal；原生label、focus-visible、窄屏和明暗tokens。
- [ ] GREEN：`cd backend && go test ./internal/handler -run TestAdvertisingGeneration -count=1`；`cd web && bun test test/advertising-generation-api.test.ts test/advertising-generation-interaction.test.tsx && bun run typecheck && bun run lint`。只格式化所改文件。提交`feat(advertising): 广告制作 - 接入报价候选与成片页面`。

### Task 8: 真实闭环验证、文档与一次独立整分支评审

**Files:** 新增web/scripts/advertising-generation-chrome-e2e.mjs（沿已有CDP脚本原生方式，不新加框架）、docs/content/docs/backend/advertising-generation.mdx；更新features.mdx、code-map.mdx、http-api.mdx、backend-database.mdx、pending-test.mdx。脚本使用隔离数据/账号，测试provider只在测试环境注入，生产无假成功开关。
**Interfaces:** 前七任务的公开合同及真实服务；全分支评审基线cea76a66至最终HEAD。

- [ ] 运行范围后端全包database/repository/handler/copier与app广告、Creation、账务、任务恢复、资源、时间线回归；测试结果区分缺runtime依赖/缺PG环境。临时SQLite执行migrate-schema up→status→verify→up，绝不backend/data。
- [ ] 运行`cd web && bun run test && bun run typecheck && bun run lint && bun run build`，检查新增文件Prettier；验证OpenAPI全部新路由、响应引用与TS合同。
- [ ] Chrome真实注册/登录→已有B确认→报价→假provider确定性视频/TTS→试听采用→音乐→组装→时间线打开→MP4下载。用明确标注TEST的非黑媒体；报价/扣款/刷新/重放/取消/超时分别断言。检查1440px明暗、390px无横向溢出、键盘焦点与弹窗。
- [ ] 若真供应商模型和预算未另授权，记录“真实AI生成验收未执行”，不是功能失败也不是通过；PG/Redis/OSS同理。列出人工听感/商品一致性/文案质量待验收，不用测试画面宣称商品广告质量。
- [ ] 一次独立只读整分支评审（无实现子代理），重点账务幂等/上限、后端旁路、完成与删除竞争、真实音轨/时长、旧画布不覆盖；所有Critical/Important必须验证并修复，补RED/GREEN。Lead看最终diff并重跑受影响检查。
- [ ] 更新验收矩阵及用户文档，提交`docs(advertising): 广告生成 - 记录闭环验收与环境边界`。检查无密钥/数据库/缓存进入Git，收集全部后台任务结果并关闭测试服务；不合并/推送，明确保留分支。

## 自查与执行交接

规格§1–4→Task3/7；§5→Task1/4/6；§6→Task2/3；§7→Task4/5；§8→Task5/6；§9→Task7；§10→Task8。已补发现的ScopeKey36长度不足、Task注册回调复用、旧render key重放以及不覆盖手改画布；无需求被静默延后。

当前计划完成，不代表实施已开始。请用户审阅并确认本计划；确认后沿保留的内联方式执行八任务，最后一次独立评审。真实付费模型仍需单独授权。
