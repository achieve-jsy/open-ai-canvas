# B：广告项目与资产快照设计

状态：方向已确认，书面规格待用户评审；不是已实现能力。基于分支 feat-advertising-render 的 1d53c857。A代码主体完成，但真实页面、容器和商品样片验收仍未闭环。

## 1. 目标与边界

面向基于开源影策的真实面试演示。B交付：创建广告 → 填写商品需求 → 选择已有素材/上传 → 人工编辑3–5镜头脚本 → 保存 → 人工确认版本 → 刷新恢复及查看历史确认版本。

范围：中文、单商品、15–30秒、1080×1920。B只管理需求、脚本、品牌快照和引用；脚本可人工录入，不提供假“AI生成”按钮。真实模型生成、TTS、候选采用、费用和任务关联属于C；品牌档案、多比例、变体属于D。不改A的渲染合同，不另建编辑器或素材存储。

## 2. 现状与方案选择

- [Project](<../../../backend/internal/model/models_project.go#L209>)已有用户归属、名称、类型、revision；[通用更新](<../../../backend/internal/repository/repository_projects.go#L60>)不带预期revision条件，不能直接当广告并发协议。
- [创建项目](<../../../backend/internal/app/project.go#L219>)无条件初始化短剧工作流，不能给type换名就当广告项目。
- [项目路由](<../../../web/src/router.tsx#L179>)与[后端注册](<../../../backend/internal/handler/project.go>)受短剧功能开关控制。广告需要自己的入口，不放开全部短剧API。
- [资源删除](<../../../backend/internal/app/resource_delete.go#L45>)区分普通删除与彻底删除。彻底删除可移除素材入口，但已被其他素材/短剧直接使用的资源会保留。广告也需直接资源引用，不能仅把resourceId藏进JSON期待所有删除路径自动保护。

选择：复用Project身份/名称/用户归属，以三张小型广告业务表保存草稿、确认版本、资源引用。复用资源上传、资源读取和清理队列。

不选“把全部广告状态塞进StyleProfileJSON”：语义冲突，且不能可靠锁定确认版本。不选“重建独立项目和素材平台”：重复权限、上传、任务归属和删除系统。

## 3. 数据模型

以下为拟议合同，不在本轮修改schema。实施前使用Atlas工作流，验证SQLite/PostgreSQL迁移及备份迁移工具的表清单。

### 3.1 广告草稿 advertising_projects

- projectId：主键，关联既有Project；Project.type固定为advertising。
- revision：从1递增的广告乐观锁，独立于Project.revision，后者仍作为通用项目更新时间版本。
- draftJson：规范化的广告内容，schemaVersion=1。
- latestConfirmedRevisionId：可空，仅指向同项目确认版本。
- createdAt / updatedAt。

广告名称保存在Project.name，按既有“同用户名称唯一”约束；广告保存时与草稿同事务更新。需求/品牌不放入Project.description或StyleProfileJSON。

### 3.2 确认版本 advertising_revisions

- id、projectId、sourceRevision、snapshotJson、createdAt。
- unique(projectId, sourceRevision)，同一保存修订最多确认一次。
- snapshotJson包含确认时的广告名称、完整规范化draftJson、资源标题/MIME/尺寸/时长元数据快照。
- 只新增、只读；不提供单版本修改/删除API。修改确认内容只能编辑草稿后另行确认。删除整个广告时可删除其历史版本。

### 3.3 资源绑定 advertising_resource_references

- projectId、scopeKey、resourceId；复合唯一键。scopeKey为draft或确认版本ID。
- 资源引用按每份草稿/版本去重；具体用途、镜头、展示标题保存在内容JSON，不重复建立镜头资产表。
- projectId指向广告实体；resourceId指向现有Resource并禁止仍被引用时删除资源行。scopeKey所属关系由同事务服务写入控制。
- resourceId是持久媒体身份；来源assetId/assetVersionId只作来源说明，不能作为运行时唯一解析依赖。素材入口被彻底删除后仍可由resourceId读取确认内容。

草稿引用随保存整份替换；确认时新增该版本的引用，不移动/覆盖草稿引用。历史版本引用一直保留到删除整个广告。

## 4. 内容合同与校验

所有长度按Unicode码点；限制请求正文256KiB；拒绝未知结构字段、非有限/非整数时长、非法ID及资源字段中的URL/宿主路径；普通文案中的URL按字面文本处理，不访问。预览URL由资源服务生成，不持久化临时签名地址。允许保存不完整草稿，但所有已填值必须格式有效、资源必须属于当前用户且就绪。

| 内容 | 草稿限制 | 确认要求 |
| --- | --- | --- |
| name | 必填，1–120字 | 同左 |
| brief.productName | 0–120字 | 非空 |
| brief.description | 0–2000字 | 可空 |
| brief.audience | 0–300字 | 非空 |
| brief.sellingPoints | 0–5条，每条1–120字 | 至少1条 |
| brief.cta | 0–80字 | 非空 |
| brief.durationMs | 整数15000–30000，默认15000 | 镜头时长之和严格等于该值 |
| brand.name / primaryColor | 名称0–120字；颜色可空或#RRGGBB | 可空，不填不推断 |
| brand.logoResourceId | 可空，PNG/JPEG/WebP | 填写时须可读取 |
| productResourceIds | 0–10张PNG/JPEG/WebP商品图，去重 | 至少1张 |
| additionalResourceIds | 0–20个图片/视频/音频资源 | MIME限定image/png、image/jpeg、image/webp、video/mp4、audio/mpeg、audio/mp4、audio/wav、audio/x-wav |
| shots | 0–5条；顺序即播放顺序；id为客户端UUID，同文档唯一 | 3–5条 |
| shot.durationMs | 整数1000–15000 | 同左且总时长正确 |
| shot.visual / voiceover / onScreenText | 分别0–1000、0–500、0–200字 | visual非空，其他可空；onScreenText最多两行 |
| shot.referenceResourceIds | 0–5个ID | 必须来自本稿的productResourceIds或additionalResourceIds |

服务端回读资源元数据并生成快照，不信任客户端提交的MIME/尺寸/标题。品牌Logo仅作为品牌资源，不自动混入镜头参考。删除或替换商品素材时仍被镜头引用则返回校验错误，UI提示先解除镜头引用，不静默删镜头。

B不保证配音刚好填满镜头，也不宣称文案合规；C负责实际音频时长和生成能力校验。B不把有Logo/配音列为确认硬门槛，最终完整广告验收仍按总规格。

## 5. 保存、确认与并发

- 创建：一个事务写Project与广告草稿及引用，不创建WorkflowInstance/ProjectUnit。任一步失败完整回滚。
- 保存：提交expectedRevision与整份{name,draft}；归属与格式检查后事务内再次校验资源，CAS更新广告revision，替换draft引用、同步Project.name/updatedAt/revision。更新0行返回409；不得最后写入覆盖。
- 确认：只提交expectedRevision，不夹带未保存文本。校验当前已保存稿完整性，在事务内锁定/CAS当前广告修订，新增不可变快照和引用、设置latestConfirmedRevisionId，并将广告revision递增1。
- 重复确认：同projectId和sourceRevision已有确认记录时，返回原确认版本，不重复创建，即使当前草稿已后续修改也不改变最新指针。不存在旧确认且expectedRevision已过期则409。
- 对当前revision再次确认但内容与最新确认快照相同，返回最新确认版本，不增加修订。以规范化{name,draft}比较，不以会变化的资源元数据判定文案变化。
- 保存会保留latestConfirmedRevisionId；UI比较规范化{name,draft}与最新确认内容，确有不同才显示“当前草稿有修改，历史确认仍有效”。再次确认有变化的草稿才产生新版本。确认时的revision和生成时任务绑定修订不能混用，C使用确认版本ID。
- 保存/确认/删除同项目采用同一数据库行锁/CAS顺序；SQLite使用其事务能力，PostgreSQL使用行锁，不以进程内mutex冒充多实例保证。
- 网络失败保留本地输入，刷新读取服务端；确认响应丢失可用同expectedRevision重试。保存响应不明先重新读取，不静默重放旧修改。

## 6. 资源与项目删除

绑定与资源删除必须在数据库中形成安全约束：保存/确认事务校验Resource归属/状态并写显式引用；资源删除前重查引用，并由resourceId的限制删除外键作为最后保障。资源对象只允许在资源记录删除及Outbox提交后清理，禁止先删物理对象再发现引用冲突。若当前驱动未启用FK，需要先在迁移/运行配置和测试中落实，不得降级为仅做一次应用层检查。

复用现有语义：普通素材删除返回广告占用来源；用户彻底删除素材库入口可以成功，但广告仍引用的Resource与物理文件保留。将广告直接引用同时接入普通引用快照、彻底删除分支及后台孤儿清理；不能只修改其中一个调用方。不得自动复制物理文件来规避引用管理。

删除广告只删除其草稿、历史版本和广告绑定，解除已有项目关系；不直接删除素材库入口和资源。其他广告、画布或历史任务仍使用的文件不受影响，无引用资源由既有清理规则处理。存在排队/运行任务时沿用现有项目删除拒绝规则。删除携带expectedRevision，和保存竞争时只允许一个赢。

旧通用项目API不得绕过广告合同：禁止用通用创建接口构造advertising项目、禁止把现有类型切换为/切换出advertising；通用更新和短剧工作流写入对广告项目拒绝。通用删除入口对广告项目拒绝，要求带revision的广告删除接口。保留资源/任务/画布按真正Project ID查归属的共享能力。短剧列表排除advertising，广告列表只查询广告实体；不是只靠前端过滤。

## 7. HTTP合同

路径相对/api，统一登录鉴权和现有响应信封；通过handler → service别名 → app → repository/model。类型在业务API模块定义，前端调用现有http。独立/ad-projects路由不受shortDramaEnabled控制，不新增无实际需求的功能开关。

| 方法与路径 | 请求 | 返回 |
| --- | --- | --- |
| GET /ad-projects | page默认1，pageSize默认20、最大100 | items、page、pageSize、total；仅摘要，不含全部版本JSON |
| POST /ad-projects | {name,draft}，省略draft时使用空稿默认值 | 广告详情与revision |
| GET /ad-projects/:id | — | Project摘要、draft、revision、latestConfirmedRevisionId、资源可用状态 |
| PUT /ad-projects/:id | {expectedRevision,name,draft} | 最新详情 |
| POST /ad-projects/:id/confirm | {expectedRevision} | 不可变确认版本及当前广告revision |
| GET /ad-projects/:id/revisions | 同列表分页 | 确认版本摘要列表 |
| GET /ad-projects/:id/revisions/:revisionId | — | 该确认版本，含资源快照 |
| DELETE /ad-projects/:id | expectedRevision查询参数 | {deleted:true} |

列表按updatedAt倒序、projectId倒序稳定排序；版本列表按sourceRevision倒序。创建成功但响应丢失时先查列表，名称唯一约束阻止同名重复创建；B不新增通用幂等服务。资源读取/上传走既有API。未经授权对象统一404；未登录401；结构/完整性错误400并提供字段错误；名称冲突、修订冲突、资源被引用、任务占用409。复用已有reason，缺少的新增稳定机器码，不按中文msg分支。数据库/存储故障真实5xx，不返回空稿当成功。新增reason、响应类型和OpenAPI随实现同步。

## 8. 页面与恢复

真实路由/ad-projects和/ad-projects/:id，使用现有登录壳与导航增加“广告项目”；不借用短剧详情页、不建立演示HTML、不依赖未挂载的独立编辑器。

列表：名称、商品名、目标时长、是否已有确认版、更新时间；创建用AppModal。详情：需求/品牌/素材、按顺序镜头编辑、确认版本历史三个区域；窄屏纵向排列。素材选择复用现有素材库/上传，按resourceId预览。时间线精修/媒体生成不在B页面伪装可用。

显式“保存草稿”和“确认脚本”；有未保存更改时确认按钮不可直接提交，先保存。显示保存中/已服务端保存/保存失败/冲突，不把浏览器缓存当云端持久化。冲突保留本地文本，提供查看服务端版本与复制本地内容，不自动合并。首版不做自动保存与离线同步；离开脏表单警告。

切换账号/项目后丢弃旧请求回写，查询键带用户scope。全部控件有可访问标签、键盘焦点、可点击排序按钮；拖拽不是镜头排序唯一方式。复用主题token，支持明暗和窄屏。前端实施前加载ui-ux-pro-max。

## 9. 必须通过的验收

1. 新建广告不生成短剧工作流；短剧开关关闭时广告读写可用，短剧原限制仍有效。
2. 保存后重新读取/真实页面刷新恢复需求、镜头顺序、品牌和资源；本地未保存内容不会假称已云端保存。
3. 两个客户端同expectedRevision保存仅一个成功，另一个409；事务失败无半个项目、半份快照或悬空引用。
4. 确认严格验证3–5镜头、15–30秒、资源归属和字段；重复确认返回同ID；修改草稿/品牌不改变旧确认JSON或引用。
5. 未登录、跨用户、非广告ID、跨项目版本ID、恶意URL、超长/重复字段、通用项目API绕过均拒绝。
6. 两广告共享同资源：删一广告另一方仍可读；彻底删素材入口不会损坏广告快照依赖；普通删除报告引用。并发绑定/删除不能产生成功保存却已丢文件的快照。
7. 真实SQLite与PostgreSQL迁移/约束及升级旧数据验证；未执行的环境明确记录，不以SQLite通过替代PostgreSQL。
8. 真实页面键盘、窄屏、明暗、错误状态验收；未授权启动服务时只记录自动测试依据，不宣布浏览器验收通过。
9. 无模型调用、无账单、无新增镜头生成任务；B展示“已确认脚本”，绝不展示“广告已生成”。

## 10. 下一步与交付边界

本规格确认后再编写函数/文件/测试级实施计划。沿用用户此前选择的内联执行意向，但实施计划仍需评审；迁移必须走Atlas，不手改版本号，不动既有部署数据。开发/测试使用隔离数据目录，不启动未授权服务、不推送、不收费调用。

C只消费不可变确认版本ID再设计生成、采用、恢复和费用合同；此处不预建生成批次、通用事件总线或品牌平台。B完成也不等于广告全链路完成。
