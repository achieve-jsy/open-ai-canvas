# B 广告项目与资产快照 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. 用户已选择Lead内联实现，最后一次独立全分支评审；不重新委派逐任务实现。Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** 交付创建广告、商品需求/品牌/素材绑定、人工脚本编辑、版本确认与刷新恢复，不调用生成模型。

**Architecture:** 复用Project身份及Resource上传/读取/删除系统。三张广告表承担草稿CAS、不可变版本和显式资源引用；广告独立路由，不初始化短剧工作流。按现有handler→service别名→app→repository/model组织，不引入新框架或通用编排器。

**Tech Stack:** React19、TS、Bun、AntD、TanStack Query；Go/Gin/GORM、SQLite/PostgreSQL；现有migrate-schema与资源删除Outbox。

**Spec:** [已确认B规格](<../specs/2026-11-04-advertising-project-design.md>)。本计划待用户评审；尚未实施。用户已明确批准用版本化GORM迁移替代规格最初的Atlas要求。

## Global Constraints

- 中文、单商品、15–30秒、3–5镜头、1080×1920；所有字段限制使用规格§4原表，不在各层发明不同上限。
- 三张表：advertising_projects、advertising_revisions、advertising_resource_references。Project.type=advertising；禁止把广告内容塞入style JSON。
- JSON schemaVersion=1；正文256KiB；UTF-8码点限长，有限整数时长，未知字段/重复JSON键/第二个JSON值拒绝。普通文本URL按字面保存，资源字段只收ID。
- 资源归属、状态、MIME由服务端查验；只有resource:后端ID可从现有素材解析用于绑定。本地上传降级不是已保存。
- 确认版本只新增/只读；保存、确认、删除按expectedRevision竞争；生成/候选/预算/TTS/批次属于C/D，不预建表或按钮。
- 不新增JS依赖；不改原部署数据、不启动未授权服务/容器、不收费调用、不推送。测试和本地构建属于计划验证；生产/共享库禁止用于测试。
- 当前schema版本78只是调研快照；执行Task1时重新核对最大版本并追加，严禁改已发布迁移。独立worktree继续使用feat-advertising-render，遇用户变更先保留。
- 数据与资源错误不默认成功；新API理由码和OpenAPI同步。前端使用http，不自建fetch/axios JSON协议。

## Review Focus

1. 普通/彻底删除与后台清理三条路径都必须保护广告资源；绑定和物理删除并发由FK与事务兜底（Tasks1/3）。
2. 重复确认响应丢失、确认后草稿又修改不能把latest指针倒退或创造空变更版本（Task2）。
3. 既有项目创建/更新/删除、短剧子路径不得绕过广告修订；短剧开关关闭仍能读写广告（Task4）。
4. 只在本机上传成功、远端失败时不能绑定/确认；旧用户异步响应不能污染新用户页面（Task5）。
5. SQLite建表通过不证明PG FK/锁和跨库搬迁正确；两驱动与历史schema升级单独记录（Tasks1/6）。

## 文件职责与依赖

新增路径在本计划中是拟创建文件；测试沿仓库现有Go testing/Bun，不加框架。

| 任务 | 新增文件 | 修改既有落点 |
| --- | --- | --- |
| 1 模型/迁移 | backend/internal/model/models_advertising.go；backend/internal/database/advertising_migration_test.go | backend/internal/database/schema.go、migrations.go；backend/cmd/migrate-sqlite-postgres/main.go、main_test.go；docs/content/docs/backend/backend-database.mdx |
| 2 业务 | backend/internal/app/advertising_project.go、advertising_project_validation.go、advertising_project_test.go；backend/internal/repository/repository_advertising.go | 复用repository.Transaction、CreateProject、ProjectForUser、资源查询，不复制通用上传 |
| 3 引用/删除 | backend/internal/app/advertising_resource_test.go | 上述业务/仓储；backend/internal/repository/resource_reference.go、resource_cleanup.go、repository_storage_resources.go、repository_projects.go；backend/internal/app/resource_delete.go |
| 4 HTTP/隔离 | backend/internal/handler/advertising_project.go、advertising_project_test.go；backend/internal/service/aliases_advertising.go | backend/internal/handler/api.go、openapi.yaml；backend/internal/app/project.go及查出的短剧写入调用方；repository_projects.go；kernel/error_codes.go及已有别名/前端error-codes.ts |
| 5 UI/API | web/src/services/api/advertising-project.ts；web/src/pages/ad-projects/index.tsx、detail.tsx、advertising-draft-form.tsx；web/test/advertising-project-api.test.ts、advertising-project-interaction.test.tsx | web/src/router.tsx；web/src/components/layout/workspace-sidebar-nav.tsx |
| 6 验收 | docs/content/docs/backend/advertising-project.mdx | docs/content/docs/overview/features.mdx、backend/code-map.mdx、progress/pending-test.mdx、http-api.mdx |

依赖1→2→3→4→5→6。Task2不暴露HTTP，待删除守卫和通用路径防绕过完成后才注册路由。同一文件不要让多名实现者并发编辑。

## Task 1：三张模型、原生迁移和删除约束

**Interfaces**（models_advertising.go）：AdvertisingProject{ProjectID,Revision,DraftJSON,LatestConfirmedRevisionID,CreatedAt,UpdatedAt}；AdvertisingRevision{ID,ProjectID,SourceRevision,SnapshotJSON,CreatedAt}；AdvertisingResourceReference{ProjectID,ScopeKey,ResourceID}。ID与既有Project/Resource长度一致；latest为空用NULL。引用复合PK(project_id,scope_key,resource_id)，版本唯一(project_id,source_revision)；refs.resource_id为显式RESTRICT FK，索引resource_id；refs/project和version/project只允许按广告删除事务正确清理。latest指针由业务事务维护，避免建循环FK造成搬迁死锁。

- [ ] **RED:** 在advertising_migration_test.go新增TestAdvertisingMigration：当前旧版本库升级后应有三表/索引/FK；引用存在时删除Resource失败，删除引用后可删除；重跑迁移无变更，历史migration checksum不变。新库也验证；使用database.Open及临时目录确保foreign_keys=on，不用默认关闭FK的裸内存连接冒充通过。
- [ ] **Run:** cd backend && go test ./internal/database -run TestAdvertisingMigration -count=1；预期缺模型/约束失败，记录实际RED。
- [ ] **Implement:** 注册三模型到Models；追加advertising_projects迁移并更新CurrentSchemaVersion；校验只创建新对象、无旧表数据重写。GORM association若不能可靠产生命名RESTRICT FK，在新增迁移内按两驱动创建对应约束，不修改已有Resource schema定义或历史迁移。
- [ ] **Transfer:** 在migrate-sqlite-postgres显式清单把广告实体排在Project/Resource之后，再排确认版本和引用；补覆盖性及一条含确认版本的搬迁夹具，避免模型注册了却漏复制数据。
- [ ] **PG:** 增加TestAdvertisingPostgresMigration，沿schema_postgres_test.go用CANVAS_TEST_POSTGRES_DSN及随机独立schema；验证FK、唯一键、迁移幂等。未配置可skip，但交付必须标明未覆盖，禁止声称双库通过。
- [ ] **GREEN:** cd backend && go test ./internal/database ./cmd/migrate-sqlite-postgres -count=1；应无失败，PG实际执行/skip分开报告。更新数据库专题三表及新迁移说明。
- [ ] **Commit:** feat(advertising): 广告项目 - 增加版本快照与资源引用模型。

## Task 2：草稿与确认的事务状态机

**Interfaces**（advertising_project.go）：AdvertisingDraft与AdvertisingShot精确采用规格§4；AdvertisingDetail{project,draft,revision,latestConfirmedRevisionId,resources}；AdvertisingSnapshot{name,draft,resources}；AdvertisingRevisionDetail{id,projectId,sourceRevision,snapshot,createdAt}；AdvertisingConfirmation{snapshot:AdvertisingRevisionDetail,currentRevision}。resources元数据从服务端读取，快照从此冻结。列表摘要不携带DraftJSON，分页结构{items,page,pageSize,total}。

服务方法：ListAdvertisingProjects(userID string,page,pageSize int)(AdvertisingList,error)；CreateAdvertisingProject(userID string,req AdvertisingCreateRequest)(AdvertisingDetail,error)；GetAdvertisingProject(userID,projectID string)(AdvertisingDetail,error)；SaveAdvertisingProject(userID,projectID string,req AdvertisingSaveRequest)(AdvertisingDetail,error)；ConfirmAdvertisingProject(userID,projectID string,expectedRevision int64)(AdvertisingConfirmation,error)；ListAdvertisingRevisions(userID,projectID string,page,pageSize int)(AdvertisingRevisionList,error)；GetAdvertisingRevision(userID,projectID,revisionID string)(AdvertisingRevisionDetail,error)。请求类型对应规格§7；save含{name,draft,expectedRevision}。

校验函数：validateAdvertisingDraft(draft AdvertisingDraft,confirm bool) error，纯结构限制；normalizeAdvertisingDraft(draft AdvertisingDraft) AdvertisingDraft，只规范空数组和约定默认值，不删非法镜头/未知资源。资源数据库校验在事务内，由app调用repo查询；ID/颜色/时长校验不能只留前端。

Repository只提供数据操作：AdvertisingProjectForUser(userID,projectID string)(*model.AdvertisingProject,error)；AdvertisingRevisionForSource(projectID string,sourceRevision int64)(*model.AdvertisingRevision,error)（仅在归属已验证事务中调用）；AdvertisingRevisionForUser(userID,projectID,revisionID string)(*model.AdvertisingRevision,error)；CASAdvertisingProject(projectID string,expectedRevision int64,updates map[string]any) error；ReplaceAdvertisingReferences(projectID,scopeKey string,resourceIDs []string) error。均复用repo.Transaction；CAS写入字段由app内部固定组装，禁止透传HTTP map。CAS结果0行有明确冲突错误。PG锁广告行，SQLite事务内先对目标行做带归属条件的同值UPDATE取得写锁，再读/检查；只有真正保存/创建确认时才CAS递增revision，以保持重复确认no-op。严禁读完再无条件Save覆盖。SQLite锁忙重试最多3次，仅重试驱动明确的锁忙错误，每次重开事务并保留原expectedRevision。

- [ ] **RED:** TestAdvertisingDraftValidation用表驱动覆盖规格§4每个上下界、Unicode、超2行、重复镜头ID、资源池外参考、未知schemaVersion。空稿能存但不能确认，三镜头总15000ms可确认。
- [ ] **RED:** TestAdvertisingProjectLifecycle：创建只有Project+广告行，无Workflow/Unit；保存同revision仅一次成功；品牌/脚本修改不改旧快照；跨用户和跨项目版本ID均拒绝。确认同sourceRevision返回同ID；响应丢失后旧revision重试不倒退latest；当前内容未变化再次确认不增加版本；过期且无历史确认返回冲突。
- [ ] **Run:** cd backend && go test ./internal/app -run 'TestAdvertisingDraftValidation|TestAdvertisingProjectLifecycle' -count=1；先记录缺实现RED。
- [ ] **Implement:** 一个事务完成身份、CAS、资源检查与绑定。创建调用repo.CreateProject而不是svc.CreateProject（后者初始化短剧）。snapshot比较只用规范化{name,draft}，不比较资源元数据更新时间。确认创建前再次校验资源；冻结后展示不得被素材名称改变而覆盖。
- [ ] **Failures:** TestAdvertisingProjectRollback通过现有GORM回调模式在版本/引用写入强制失败，断言revision/name/latest/refs均回滚。并发测试同expectedRevision保存/确认互斥（删除并发在Task3加入）；锁忙可有界重试事务，但不能重新读取revision后悄悄当成成功。
- [ ] **GREEN:** cd backend && go test ./internal/app -run 'TestAdvertising(Draft|Project)' -count=1；SQLite全部通过；PG复用临时schema环境跑相同事务用例，缺环境记待验收。
- [ ] **Commit:** feat(advertising): 广告项目 - 实现草稿修订与不可变确认。

## Task 3：广告删除和三条资源保护路径

**Interfaces:** Service.DeleteAdvertisingProject(userID,projectID string,expectedRevision int64) error；Repository.AdvertisingResourceReferences(userID string,resourceIDs []string)([]ResourceDirectReference,error)，Kind明确为广告项目，Title为当前项目名。草稿/版本scope用于错误提示定位，不泄露他人项目。新的引用查询同时接入一般快照、彻底删除、孤儿清理。

- [ ] **RED:** TestAdvertisingResourceProtection：两广告绑定同资源，普通删除返回占用；彻底删素材入口后资源仍可读；删除广告A不破坏B；仅历史确认还引用时也保护；只删草稿引用不能误删历史引用。
- [ ] **RED:** TestAdvertisingDeleteRevisionAndTasks：旧revision拒绝；存在活动项目/关联画布任务拒绝；删除只解除广告和关联，独立画布/素材/Resource保留。事务中再次检查活动任务，不能只信应用预检查。
- [ ] **Run:** cd backend && go test ./internal/app -run 'TestAdvertising(Resource|Delete)' -count=1；先验证失败。
- [ ] **Implement:** 在resource_reference.go的ResourceReferenceSnapshot、OtherAssetResourceReferences相关彻底删除路径合并广告引用；resource_delete.go把广告直接引用作为必须保留的资源，但普通删除报告占用。避免把所有引用都当可强删。后台cleanupDetachedUserResources继续复用统一快照，不写广告专用清理worker。
- [ ] **Transactional guard:** 检查DeleteAssetsAndResources、DeleteDetachedResources、DeleteResource的所有调用者；事务内最后复查广告绑定，FK冲突转成稳定资源占用错误，Outbox与资源删除同事务。测试删除先赢则绑定失败；绑定先赢则资源删除失败且无物理删除Job。
- [ ] **Project delete:** 从既有DeleteProject抽出必要的内部删除编排供广告入口复用：保持关联画布解绑和任务检查；在同一repository事务中CAS广告revision、删广告引用/版本/实体，再调用既有项目删除逻辑。公共通用DeleteProject仍留给Task4拒绝广告，不通过调用公共方法绕过revision。
- [ ] **GREEN:** cd backend && go test ./internal/app -run 'TestAdvertising(Resource|Delete)|TestDeleteProject|Test.*Resource.*Delete|Test.*Detached' -count=1 && go test ./internal/repository -run 'Test.*(Resource|Project)' -count=1。真实PG并发场景单独记录。
- [ ] **Commit:** fix(advertising): 广告项目 - 保护快照引用与并发删除。

## Task 4：HTTP、类型别名与短剧隔离

**Interfaces:** RegisterAdvertisingProjectRoutes(r *gin.RouterGroup,svc *service.Service)，api.go在登录业务路由组注册，不套RequireShortDrama。八条路径按规格§7；广告详情读取需拥有Project且广告实体存在。新增aliases_advertising.go只type alias，不建透传service实例。

- [ ] **RED:** TestAdvertisingRoutes用现有handler测试登录夹具覆盖8条路径、401/404、分页最大100、短剧开关关闭广告仍可用。TestAdvertisingStrictJSON覆盖256KiB边界、未知字段、重复键（包括嵌套）、尾随JSON、null正文和revision非正数。
- [ ] **RED:** TestAdvertisingCannotBypassRevision：通用CreateProject不能type=advertising；通用更新不能改入/改出此类型，广告不能走通用Delete或短剧Unit/Workflow/Shot/Drama写入口；原短剧与资源归属测试保持通过。
- [ ] **Run:** 在backend目录分别运行go test ./internal/handler -run TestAdvertising -count=1及go test ./internal/app -run TestAdvertisingCannotBypassRevision -count=1；分别记录RED，不以首个失败掩盖另一项。
- [ ] **Implement:** 复用MaxBytesReader与json.Decoder.DisallowUnknownFields，额外一次Decoder.Token遍历检查每个对象重复键并校验EOF，局部decodeAdvertisingBody函数仅服务8条广告路由。不添加全站JSON中间件。校验错误包含字段路径文本；前端以reason识别类别，不解析msg恢复结构。
- [ ] **Errors:** 复用project_name_conflict；新增advertising_revision_conflict、advertising_invalid_document、advertising_resource_unavailable、advertising_resource_in_use、advertising_active_tasks，分别映射409/400/400/409/409，资源外链归属错误不返回他人元数据；读取广告/确认版本遇未授权对象统一404；保存引用资源失效仅报advertising_resource_unavailable，不泄露对象是否属于他人。kernel定义并沿现有别名传递，TS error-codes同步。字段提示msg保留完整路径；不改全站信封结构。
- [ ] **Isolation:** grep所有svc.activeProjectForUser/ProjectForUser/Project.Type使用；把短剧专属写入落到拒绝advertising的共享守卫，不能全局修改repo.ProjectForUser，否则A渲染任务归属被破坏。Projects/ProjectsPage短剧列表后端排除广告，广告列表独立查询且total一致；共享画布/资源归属仍允许真实广告Project ID。
- [ ] **Docs/GREEN:** OpenAPI补8条路径、schema/必填/上限/错误/示例；cd backend && go test ./internal/handler -run 'TestAdvertising|TestShortDrama' -count=1 && go test ./internal/app -run 'TestAdvertising|TestCreateProject|TestDeleteProject|TestTimelineRenderAccess' -count=1。
- [ ] **Commit:** feat(advertising): 广告项目 - 开放接口并隔离短剧写路径。

## Task 5：真实广告页面与保存恢复

**Interfaces:** advertising-project.ts导出与Task2响应逐字段一致的TS类型及list/create/get/save/confirm/listRevisions/getRevision/deleteAdvertisingProject；API用http，查询用现有compactApiParams。AbortSignal保留；确认返回冻结snapshot与当前revision，不错用sourceRevision。

页面路由/ad-projects和/ad-projects/:id位于现有RequireAuth/UserLayout，不依赖shortDrama开关。AdvertisingDraftForm props={value:AdvertisingDraft,onChange:(next:AdvertisingDraft)=>void,disabled?:boolean}只编辑内容；detail负责查询、草稿、保存/确认/冲突。标题修改也参与脏状态。不要新建全局广告store。

- [ ] **Preflight:** 加载ui-ux-pro-max并读docs/ui-design-system.md。读取完整AssetLibraryPickerModal props、uploadMediaFile及task snapshot资源解析模式；按实际asset.data.storageKey解析，不猜字段。新的页面使用既有WorkspacePage/AppModal与主题token。
- [ ] **RED:** advertising-project-api.test.ts断言路径/正文/expectedRevision、http业务错误上抛。advertising-project-interaction.test.tsx挂真实页面/表单，mock API边界而不是替身HTML：空稿保存、三镜头顺序、未保存不能确认、失败保留输入、409保留本地与显示服务端版本、确认成功更新revision、重载按后端数据恢复。
- [ ] **Run:** cd web && bun test test/advertising-project-api.test.ts test/advertising-project-interaction.test.tsx；先记录缺实现RED。沿现有DOM隔离模式，避免SSR import缓存污染；不用source.includes断言代替业务交互。
- [ ] **Implement list:** 列表摘要分页、创建AppModal、打开详情、删除二次确认（带revision，冲突不从列表假删除）；导航加“广告项目”，active匹配/ad-projects。后端列表按updatedAt/projectId稳定倒序。
- [ ] **Implement detail:** 原生标签字段和数字毫秒/秒显式转换，三个区域需求品牌素材/镜头编辑/确认历史；镜头UUID稳定、上移下移按钮及删除提示，严格引用池。保存/确认有pending互斥，保存成功使用服务端最新规范化数据；历史版本只读，脏状态离开拦截，普通文本React渲染。
- [ ] **Assets:** 复用AssetLibraryPickerModal remoteLibrary及pickedItems能力；Logo/商品仅选指定图片MIME，其他素材严格允许列表。uploadMediaFile返回pendingRemoteUpload或非resource:键时显示“仅本地，尚未可绑定”，不添加到服务端草稿；从上传结果取得ResourceID，不从blob URL推断。允许重试既有上传流程，不删除既有本地缓存。
- [ ] **Race checks:** query key包含active用户scope+projectID；请求取消及响应归属检查避免旧请求覆盖新页面。新上传响应晚到、编辑中后台refetch、确认响应丢失均保留正确草稿，不自动收费/重提交。增加账号切换/项目切换及本地上传降级交互测试。
- [ ] **GREEN:** cd web && bun test test/advertising-project-api.test.ts test/advertising-project-interaction.test.tsx && bun run typecheck && bun run lint && bun run build。只格式化本次文件；无真实浏览器环境则明确记录未覆盖。
- [ ] **Commit:** feat(advertising): 广告项目 - 接入需求脚本编辑与确认页面。

## Task 6：整体证据、文档与独立评审

- [ ] **Regression:** backend运行go test -p 2 ./internal/database ./internal/repository ./internal/handler ./cmd/migrate-sqlite-postgres以及go test -p 2 ./internal/app -run 'TestAdvertising|TestTimelineRender|TestDeleteProject|Test.*Resource.*Delete|Test.*Detached|TestShortDrama' -count=1；web运行bun run test、bun run typecheck、bun run lint、bun run build。记录前置依赖缺失，不把已知Agent全量缺依赖说成通过。
- [ ] **Migration evidence:** 在新临时目录运行CANVAS_BACKEND_DATA_DIR=<明确临时绝对目录> go run ./cmd/migrate-schema up，再status、verify，再次up证明幂等；旧版本临时库升级测试保留旧数据。CANVAS_TEST_POSTGRES_DSN仅指专用测试库，测试创建/删除随机schema；未配置列为待验收，不自动启动容器或访问生产。
- [ ] **Browser gate:** 另行获准启动真实应用后才执行登录→创建→上传→三镜头→保存→确认→刷新→修改草稿→旧版不变→删除共享素材保护，检查明暗/窄屏/键盘/网络中断。B不生成MP4，因此A样片不是B全链路证据；不开替身HTML。
- [ ] **Docs:** advertising-project.mdx写业务边界、保存/冲突/确认语义、资源删除、迁移操作与实际验证；更新features/code-map/http-api/pending-test。规格§9逐项标“自动通过/人工通过/未执行”，不笼统写B全部完成。
- [ ] **Review:** 对本计划基线到当前HEAD发起一次fresh独立全分支评审；优先查CAS、删除竞态、资源归属、短剧旁路、双库约束。Important必须用RED/GREEN修复；如评审发现新竞态不得仅补ctx检查冒充事务保证。
- [ ] **Commit/handoff:** docs(advertising): 广告项目 - 记录验收与阶段边界；列提交、真实测试、Rulings I made、Deferred minors。保持worktree和分支，不默认合并/推送；推送另需用户授权与verify-before-push。

## 自查结论

规格§1–3→Tasks1/2/5；§4→Tasks2/4/5；§5→Task2；§6→Tasks1/3/4；§7→Task4；§8→Task5；§9–10→Task6。五项Review Focus均有对应测试，不预留空实现。

迁移机制已按用户确认纠正为现有GORM版本链。待验收环境是PG/浏览器的执行条件，不是授权跳过约束；无法运行时不能宣称相应验收完成。当前仅计划编写，无业务代码、数据库迁移、服务启动、收费调用或推送。
