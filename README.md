# 化石带议庭（Fossil Court）

多剖面地层研究的本地议事服务：从多条剖面导入化石分类单元的**出现 / 未见 / 重工可能性 / 采样层段**，
用**首现（FAD）/ 末现（LAD）/ 共存（COEX）事件**建立分带，并在保留缺失、重工与分类异议的前提下
对齐不同剖面。技术栈：Go + 纯 Go SQLite（`modernc.org/sqlite`，无需 cgo）+ `net/http` + 服务端 SVG。

## 安装与演示

```bash
go mod download
go test ./... -count=1 && go run ./cmd/server --listen 127.0.0.1:5570
# 浏览器访问 http://127.0.0.1:5570 ，页面标题为「化石带议庭」
```

数据库默认落在工作目录 `fossil-court.db`（可用环境变量 `FOSSIL_COURT_DB` 覆盖）。
数据库为空时会自动写入固定 fixture。

## 数据口径（重要）

- **深度方向**：深度数值向下增大（深度越大层位越老）；同剖面层位序边为“深（老）→ 浅（新）”。
- **四种产出状态**严格区分，不把“没有采样”混为负证据：
  - `present` 正常产出；
  - `absent` 未见：只在**采样充分**（`sample_intervals.sufficient=1`）的层段中登记的“未见”才算**负证据**，
    可约束末现；
  - `insufficient` 采样不足：层段充分度不足时即使“未见”也**不能**作负证据；
  - `unsampled` 完全未采样：该分类单元在该剖面没有任何产出/未见记录，不产生任何 FAD/LAD 事件。
- **重工与多解释**：把异常产出标为重工候选后，事件同时保留两套口径：
  - `raw` 原始口径：使用全部产出（含重工点）；
  - `in_situ` 原位口径：剔除重工候选点。
  当两套口径使某事件相对顺序倒置时，两套分带方案与 `event_order_inversion` / `rework_ambiguity`
  软约束并列返回，**不按深度最值硬定**首现/末现。
- **版本化等义关系**：合并（merge）/ 拆分（split）都写入 `syn_versions`，永不静默覆盖；
  改等义关系后只重算**依赖事件**（组成员产出、共存对），无关事件原样保留、`revision` 不自增，
  重算范围写入 `recompute_log`。
- **锦标（等时层位对）**：锁定 `(组,FAD/LAD,剖面)` 对。成环时系统**不会删除最新锦标自动恢复**：
  参与冲突链的锦标被标记 `conflicted=1` 但全部保留，接口返回一条**具体冲突链**（每步标注是层位序边
  还是哪条锦标边）。需人工“停用”锦标才能解除。
- **分带方案**：每个剖面 × 两种解释各出一套事件组合带；另有跨剖面全球并置方案，成环时不出全球带，
  改报 `hard: tie_cycle` 与冲突链。每个方案附带它违反的软/硬约束及证据引用。

## 固定 fixture（三条剖面，同一类群三种命运）

`internal/fixture`：

| 剖面 | G-T（三叶虫等义组 TA+TB） | 说明 |
|---|---|---|
| S1 龙脊坡 | 正常产出（80–200m） | 与 U 错层/共存事件完整 |
| S2 青石崖 | TB@60m 异常高位点，预置**重工候选** | raw FAD=60 / in_situ FAD=150，与 U 的 FAD 顺序在两解释间倒置 |
| S3 白塔坪 | **完全未采样**（无任何记录） | 不产生 G-T 事件；另有 W 在充分采样层段中“未见”（absent） |

预置锦标 `TIE-SEED1`（U LAD@S1 ≡ U FAD@S2）、`TIE-SEED2`（U LAD@S2 ≡ U FAD@S3），初始不闭合。
再加入 `U FAD@S1 ≡ U LAD@S3` 即得到 7 边的具体成环冲突链（含 4 条层位序边 + 3 条锦标边）。

## 操作与 HTTP 接口

页面（`/`）左侧：采样层段/状态表、重工候选标记、等义关系合并拆分、锦标锁定/停用、
导出/清空重灌/导入；右侧：双解释事件候选（含证据与依赖产出）、分带方案与违反约束、成环冲突链、
重算日志。另有服务端生成的 SVG 剖面图 `GET /svg/sections`。

- `GET  /api/state` 全量议事状态（剖面/层段/产出/状态/事件/方案/锦标/日志）
- `POST /api/syn/merge` `{groupId?,displayName,members:[...],note?}` 建版本化并组
- `POST /api/syn/split` `{groupId,note?}` 版本化拆分
- `POST /api/rework` `{occurrenceId,marked,note?}` 标记/撤销重工候选（只重算依赖事件）
- `POST /api/ties` 锁定锦标；成环返回 `409` 与 `conflictChain`（锦标已保留）
- `POST /api/ties/{id}/deactivate` 人工停用锦标
- `GET  /api/export` 导出运行记录 JSON bundle（全部业务表 + 事件修订 + 重算/操作日志）
- `POST /api/import` **清空数据库**后导入 bundle（外键逆序清空，自增序列对齐表内最大值）
- `POST /api/reset` 清空并重灌固定 fixture

## 清空数据库后重新导入复核

```bash
# 1) 先在页面或接口上做过操作（合并/重工/锦标…），再导出
curl -s http://127.0.0.1:5570/api/export -o run.json
# 2) 停服、删除数据库（或直接调 reset 清表），重新启动后会得到空业务表
# 3) 导入此前导出的运行记录，事件修订、成环标记、等义版本史与重算日志全部复原
curl -s -X POST http://127.0.0.1:5570/api/import \
  -H 'Content-Type: application/json' --data-binary @run.json
```

自动化测试 `go test ./...` 覆盖：未见≠未采样、重工双解释倒置、等义变更只重算依赖事件、
成环返回具体冲突链且不自动删除锦标、导出/清空/重导一致性、HTTP 与 SVG 冒烟。

## 目录结构

```
cmd/server          HTTP 服务入口
internal/store      SQLite 连接与建表 schema
internal/fixture    固定 fixture（固定 ID，便于重放）
internal/engine     状态推导、双解释事件、分带、锦标图与成环检测、增量重算
internal/exchange   运行记录导出/清空导入
internal/web        HTTP 路由、操作页面与服务端 SVG
```
