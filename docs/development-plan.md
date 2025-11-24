# HD Wallet EVM 多链开发计划

## 项目概述

基于 go-starter 框架开发完整的 HD Wallet 系统，支持多个 EVM 链（以太坊主网、Polygon、BSC、Arbitrum、Optimism 等）。系统采用单一助记词 + keystore 加密存储，启动时密码解锁，用户私钥按需派生，不持久化存储。

**核心设计原则**：
1. **单一种子源**：系统只保存一个加密的助记词（keystore），所有地址都从此派生
2. **启动时解锁**：服务启动时输入密码，解密助记词并生成种子，种子保存在内存中
3. **零私钥存储**：用户私钥不保存，需要签名时从种子+路径临时计算
4. **即时清除**：私钥使用后立即从内存清除
5. **元数据存储**：数据库只保存地址、路径等元数据，不保存私钥

## 开发阶段总览

- **阶段一：基础架构**（2周）- 数据库、密钥管理、地址生成、交易签名、基础API
- **阶段二：充值模块**（2周）- 多链扫描、交易检测、确认机制、充值处理
- **阶段三：提现模块**（2周）- 热钱包管理、提现服务、风控集成、提现流程
- **阶段四：余额管理**（1周）- 余额服务、余额查询API
- **阶段五：归集和调度**（2周）- 归集服务、资金调度服务
- **阶段六：优化和测试**（2周）- 性能优化、安全增强、完整测试、文档

**总时间估算：11周**

---

## 阶段一：基础架构（2周）

### 目标
完成基础架构和核心密钥管理，实现钱包创建、地址生成、交易签名等核心功能。

### 1.1 数据库设计和迁移 ✅

#### 任务 1.1.1：设计数据库表结构 ✅
**文件**：`migrations/20251121140959-create-hd-wallet-tables.sql`

**完成情况**：
- ✅ 创建 `wallets` 表（包含 chain_id 字段）
- ✅ 创建 `keystore` 表（单一记录约束）
- ✅ 创建 `address_indexes` 表（EVM 链共享索引）
- ✅ 创建 `chains` 表（链配置表）
- ✅ 创建 `transactions` 表（包含 chain_id）
- ✅ 创建 `credits` 表（包含 chain_id）
- ✅ 创建 `withdraws` 表（包含 chain_id）
- ✅ 创建 `blocks` 表（包含 chain_id）
- ✅ 创建 `tokens` 表（包含 chain_id）
- ✅ 创建 `wallet_nonces` 表（包含 chain_id）
- ✅ 创建所有必要的索引和外键约束
- ✅ 编写 Down 迁移脚本

#### 任务 1.1.2：初始化链配置数据 ✅
**文件**：`migrations/20251121141000-init-chains-data.sql`

**完成情况**：
- ✅ 插入以太坊主网配置（chain_id=1）
- ✅ 插入 Polygon 配置（chain_id=137）
- ✅ 插入 BSC 配置（chain_id=56）
- ✅ 插入 Arbitrum One 配置（chain_id=42161）
- ✅ 插入 Optimism 配置（chain_id=10）
- ✅ 插入 Avalanche C-Chain 配置（chain_id=43114）
- ✅ 插入 Base 配置（chain_id=8453）

#### 任务 1.1.3：生成 SQLBoiler 模型 ✅
**命令**：`make sql`

**完成情况**：
- ✅ 运行 `make sql` 生成所有模型文件
- ✅ 检查生成的模型文件在 `internal/models/` 目录
- ✅ 验证模型方法是否正确生成
- ✅ 检查模型关系是否正确

### 1.2 Keystore 和种子管理 ✅

#### 任务 1.2.1：实现 KeystoreService ✅
**文件**：`internal/wallet/keystore/service.go`, `encrypt.go`, `decrypt.go`, `types.go`

**完成情况**：
- ✅ 定义 `Service` 接口（CreateKeystore, DecryptMnemonic, GetKeystore, Exists）
- ✅ 实现 `CreateKeystore` 方法（生成助记词、加密、保存）
- ✅ 实现 `DecryptMnemonic` 方法（解密 keystore、验证密码）
- ✅ 实现 `GetKeystore` 方法（查询单一记录）
- ✅ 实现 `Exists` 方法（检查 keystore 是否存在）
- ✅ 实现加密逻辑（scrypt KDF + AES-128-CTR）
- ✅ 实现解密逻辑（验证 MAC、解密）
- ✅ 定义 `Keystore` 类型结构
- ✅ 实现 `NewService` Provider 函数

#### 任务 1.2.2：实现 SeedManager ✅
**文件**：`internal/wallet/seed/manager.go`, `types.go`

**完成情况**：
- ✅ 定义 `Manager` 接口（Initialize, GetSeed, IsInitialized, Clear）
- ✅ 实现线程安全的种子存储（sync.RWMutex）
- ✅ 实现 `Initialize` 方法（从助记词生成种子，保存到内存）
- ✅ 实现 `GetSeed` 方法（返回种子副本，避免外部修改）
- ✅ 实现 `IsInitialized` 方法（检查种子是否已初始化）
- ✅ 实现种子清除机制（服务关闭时清除）
- ✅ 定义 `Manager` 结构体
- ✅ 实现 `NewManager` Provider 函数

#### 任务 1.2.3：实现启动时密码验证机制 ✅
**文件**：`cmd/server/server.go`, `internal/wallet/init.go`, `cmd/server/wallet_init.go`

**完成情况**：
- ✅ 在服务启动时检查 keystore 是否存在
- ✅ 如果不存在，生成新助记词并提示输入密码
- ✅ 如果存在，提示输入密码解密
- ✅ 实现密码输入（交互式或环境变量）
- ✅ 密码验证失败时启动失败
- ✅ 密码验证成功后初始化 SeedManager

#### 任务 1.2.4：实现密码验证地址机制 ✅
**文件**：`internal/wallet/verify.go`

**完成情况**：
- ✅ 实现密码验证地址机制（派生验证地址，与数据库对比）
- ✅ 首次启动时创建验证地址（索引 0）并保存
- ✅ 后续启动时使用密码生成相同路径的地址，与数据库中的验证地址对比
- ✅ 密码验证失败时启动失败

### 1.3 地址生成 ✅

#### 任务 1.3.1：实现 AddressService ✅
**文件**：`internal/wallet/address/service.go`, `evm.go`, `bip44.go`, `types.go`

**完成情况**：
- ✅ 定义 `Service` 接口（GetNextAddressIndex, DeriveAddress, DerivePrivateKey, GetBIP44Path）
- ✅ 实现 `GetNextAddressIndex` 方法（查询/更新 address_indexes 表，原子操作）
- ✅ 实现 `DeriveAddress` 方法（从种子+路径派生 EVM 地址）
- ✅ 实现 `DerivePrivateKey` 方法（从种子+路径派生私钥，临时使用）
- ✅ 实现 `GetBIP44Path` 方法（生成固定格式路径：m/44'/60'/0'/0/{index}）
- ✅ 实现 EVM 地址派生逻辑（BIP32/BIP44）
- ✅ 实现私钥清除机制（使用后立即清除）
- ✅ 定义相关类型结构
- ✅ 实现 `NewService` Provider 函数

#### 任务 1.3.2：实现地址索引管理 ✅
**文件**：`internal/wallet/address/service.go`

**完成情况**：
- ✅ 实现地址索引的原子递增（使用数据库事务）
- ✅ 实现索引查询逻辑（按 chain_type='evm' 查询）
- ✅ 处理并发创建钱包时的索引冲突
- ✅ 实现索引回滚机制（创建失败时）

### 1.4 交易签名 ✅

#### 任务 1.4.1：实现 SignerService ✅
**文件**：`internal/wallet/signer/service.go`, `evm.go`, `types.go`

**完成情况**：
- ✅ 定义 `Service` 接口（SignEVMTransaction）
- ✅ 实现 `SignEVMTransaction` 方法（EIP-1559 交易签名）
- ✅ 实现私钥临时派生（调用 AddressService.DerivePrivateKey）
- ✅ 实现私钥清除机制（签名后立即清除）
- ✅ 实现 EIP-1559 交易构建（chain_id, max_fee_per_gas, max_priority_fee_per_gas）
- ✅ 实现交易序列化（RLP 编码）
- ✅ 定义相关类型结构
- ✅ 实现 `NewService` Provider 函数

### 1.5 基础 API ✅

#### 任务 1.5.0：实现 WalletService（主服务） ✅
**文件**：`internal/wallet/service.go`, `types.go`

**完成情况**：
- ✅ 定义 `Service` 接口（CreateWallet, GetWallet, ListWallets）
- ✅ 实现 `CreateWallet` 方法（创建钱包，支持 chain_id）
- ✅ 实现 `GetWallet` 方法（获取钱包，支持 chain_id）
- ✅ 实现 `ListWallets` 方法（列出用户所有链的钱包）
- ✅ 实现 `NewService` Provider 函数

#### 任务 1.5.1：定义 API 规范（Swagger） ✅
**文件**：`api/definitions/wallet.yml`, `api/paths/wallet.yml`

**完成情况**：
- ✅ 定义 `PostCreateWalletPayload`（chain_id 字段）
- ✅ 定义 `CreateWalletResponse`（包含 chain_id, chain_name）
- ✅ 定义 `GetWalletAddressParams`（chain_id 查询参数）
- ✅ 定义 `GetWalletListResponse`（钱包列表）
- ✅ 定义 `PostSignTransactionPayload`（chain_id 字段）
- ✅ 定义 `SignTransactionResponse`
- ✅ 定义 `GetChainsResponse`（支持的链列表）
- ✅ 在 `api/config/main.yml` 中添加引用
- ✅ 运行 `make swagger` 生成类型文件

#### 任务 1.5.2：实现钱包创建 API ✅
**文件**：`internal/api/handlers/wallet/post_create_wallet.go`

**完成情况**：
- ✅ 实现 `PostCreateWalletRoute` 函数
- ✅ 实现 `postCreateWalletHandler` 函数
- ✅ 参数验证（chain_id 必填，验证链是否存在）
- ✅ 调用 WalletService.CreateWallet
- ✅ 错误处理（链不存在、钱包已存在等）
- ✅ 返回响应（使用 util.ValidateAndReturn）
- ✅ 在 `internal/api/handlers/handlers.go` 中注册路由

#### 任务 1.5.3：实现获取钱包地址 API ✅
**文件**：`internal/api/handlers/wallet/get_wallet_address.go`

**完成情况**：
- ✅ 实现 `GetWalletAddressRoute` 函数
- ✅ 实现 `getWalletAddressHandler` 函数
- ✅ 参数验证（chain_id 查询参数）
- ✅ 调用 WalletService.GetWallet
- ✅ 错误处理（钱包不存在）
- ✅ 返回响应
- ✅ 注册路由

#### 任务 1.5.4：实现列出用户所有链钱包 API ✅
**文件**：`internal/api/handlers/wallet/get_wallet_list.go`

**完成情况**：
- ✅ 实现 `GetWalletListRoute` 函数
- ✅ 实现 `getWalletListHandler` 函数
- ✅ 调用 WalletService.ListWallets
- ✅ 返回钱包列表（包含所有链）
- ✅ 注册路由

#### 任务 1.5.5：实现签名交易 API（测试用） ✅
**文件**：`internal/api/handlers/wallet/post_sign_transaction.go`

**完成情况**：
- ✅ 实现 `PostSignTransactionRoute` 函数
- ✅ 实现 `postSignTransactionHandler` 函数
- ✅ 参数验证（chain_id, address, to, amount 等）
- ✅ 调用 SignerService.SignEVMTransaction
- ✅ 错误处理
- ✅ 返回签名后的交易
- ✅ 注册路由

#### 任务 1.5.6：实现链配置查询 API ✅
**文件**：`internal/api/handlers/wallet/get_chains.go`

**完成情况**：
- ✅ 实现 `GetChainsRoute` 函数
- ✅ 实现 `getChainsHandler` 函数
- ✅ 查询 chains 表（只返回 is_active=true）
- ✅ 返回链列表
- ✅ 注册路由

### 1.6 Wire 依赖注入配置 ✅

#### 任务 1.6.1：配置 Wire ✅
**文件**：`internal/api/wire.go`

**完成情况**：
- ✅ 创建 `walletServiceSet`（包含所有钱包服务 Provider）
- ✅ 将 `walletServiceSet` 添加到 `serviceSet`
- ✅ 在 `Server` 结构体中添加 `Wallet *wallet.Service` 字段
- ✅ 运行 `make wire` 生成依赖注入代码
- ✅ 验证服务可以正常初始化

---

## 阶段二：充值模块（2周）

### 目标
实现完整的充值检测和入账流程，支持多链扫描和交易确认。

### 2.1 区块链扫描器 ✅

#### 任务 2.1.1：实现 ScanService ✅
**完成情况**：
- ✅ 实现 ScanService（支持多链扫描）
- ✅ 实现多链并发扫描逻辑
- ✅ 实现区块扫描逻辑（按 chain_id 区分）
- ✅ 实现区块重组检测和处理（按 chain_id 区分）
- ✅ 实现扫描进度管理（blocks 表按 chain_id 区分）
- ✅ 实现 RPC 节点管理和故障转移（每个链独立）

### 2.2 交易检测 ✅

#### 任务 2.2.1：实现交易解析 ✅
**完成情况**：
- ✅ 实现交易解析（ETH 和 ERC20）
- ✅ 实现用户地址匹配
- ✅ 实现存款交易识别

### 2.3 确认机制 ✅

#### 任务 2.3.1：实现确认机制 ✅
**完成情况**：
- ✅ 实现区块确认数计算
- ✅ 实现交易状态管理（confirmed → safe → finalized）
- ✅ 实现余额更新机制

### 2.4 充值处理 ✅

#### 任务 2.4.1：实现 DepositService ✅
**完成情况**：
- ✅ 实现 DepositService
- ✅ 实现 Credits 记录创建
- ✅ 实现充值通知机制（可选）

### 2.5 充值 API ✅

#### 任务 2.5.1：实现充值 API ✅
**完成情况**：
- ✅ 实现查询充值记录 API
- ✅ 实现查询充值中余额 API

---

## 阶段三：提现模块（2周）

### 目标
实现完整的提现流程，包括热钱包管理、风控集成和提现确认。

### 3.1 热钱包管理 ✅

#### 任务 3.1.1：实现 HotWalletService ✅
**完成情况**：
- ✅ 实现 HotWalletService
- ✅ 实现热钱包创建
- ✅ 实现 Nonce 管理
- ✅ 实现热钱包选择策略

### 3.2 提现服务 ✅

#### 任务 3.2.1：实现 WithdrawService ✅
**完成情况**：
- ✅ 实现 WithdrawService
- ✅ 实现提现请求处理
- ✅ 实现余额检查
- ✅ 实现费用计算（基础版）

### 3.3 风控集成 ⏳

#### 任务 3.3.1：实现风控集成 ⏳
**待完成**：
- [ ] 实现风控检查接口（可调用外部服务）
- [ ] 实现双重签名验证
- [ ] 实现人工审核流程

### 3.4 提现流程 ✅

#### 任务 3.4.1：实现提现流程 ✅
**完成情况**：
- ✅ 实现提现状态管理
- ✅ 实现交易签名和发送
- ✅ 实现提现确认机制

### 3.5 提现 API ✅

#### 任务 3.5.1：实现提现 API ✅
**完成情况**：
- ✅ 实现发起提现 API
- ✅ 实现查询提现记录 API
- [ ] 实现提现状态更新 API（管理员用）

---

## 阶段四：余额管理（1周）

### 目标
实现完整的余额查询和管理功能。

### 4.1 余额服务 ✅

#### 任务 4.1.1：实现 BalanceService ✅
**完成情况**：
- ✅ 实现 BalanceService
- ✅ 实现余额聚合查询
- ✅ 实现可用余额计算（扣除冻结资金）
- [ ] 实现余额历史查询
- [ ] 实现余额验证机制

### 4.2 余额 API ✅

#### 任务 4.2.1：实现余额 API ✅
**完成情况**：
- ✅ 实现查询用户总余额 API
- ✅ 实现查询代币余额详情 API
- [ ] 实现查询余额历史 API

---

## 阶段五：归集和调度（2周）

### 目标
实现资金归集和调度功能。

### 5.1 归集服务 ✅

#### 任务 5.1.1：实现 CollectService ✅
**完成情况**：
- ✅ 实现 CollectService
- ✅ 实现归集策略（余额阈值 + 定时任务）
- ✅ 实现批量归集（自动循环所有用户钱包）
- ✅ 实现归集交易签名、广播与链上回执跟踪

### 5.2 资金调度 ✅

#### 任务 5.2.1：实现 RebalanceService ✅
**完成情况**：
- ✅ 实现 RebalanceService
- ✅ 实现热钱包间资金调度（支持自动和手动触发）
- ✅ 实现调度策略和阈值配置（最小/最大余额 + 定时任务）

### 5.3 归集和调度 API ✅

#### 任务 5.3.1：实现归集和调度 API ✅
**完成情况**：
- ✅ 实现手动触发归集 API (`POST /api/v1/wallet/collect`)
- ✅ 实现查询归集记录 API (`GET /api/v1/wallet/collects`)
- ✅ 实现资金调度 API (`POST /api/v1/wallet/rebalance`)

---

## 阶段六：优化和测试（2周）

### 目标
性能优化、安全审计和完整测试。

### 6.1 性能优化 ⏳

#### 任务 6.1.1：性能优化 ⏳
**待完成**：
- [ ] 数据库查询优化
- [ ] 缓存机制实现
- [ ] 批量操作优化
- [ ] 并发处理优化

### 6.2 安全增强 ⏳

#### 任务 6.2.1：安全增强 ⏳
**待完成**：
- [ ] 安全审计
- [ ] 内存安全优化
- [ ] 访问控制完善
- [ ] 日志和监控

### 6.3 测试 ⏳

#### 任务 6.3.1：测试 ⏳
**待完成**：
- [ ] 单元测试
- [ ] 集成测试
- [ ] 端到端测试
- [ ] 压力测试

### 6.4 文档 ⏳

#### 任务 6.4.1：文档 ⏳
**待完成**：
- [ ] API 文档完善
- [ ] 部署文档
- [ ] 运维文档

---

## 当前进度总结

### 已完成 ✅
- **阶段一：基础架构** - 95% (剩余文档和部分测试)
- **阶段二：充值模块** - 100%
- **阶段三：提现模块** - 80% (完成核心流程，剩余风控和管理API)
- **阶段四：余额管理** - 90% (完成核心查询，剩余历史查询)

### 进行中 ⏳
- **阶段三：提现模块** (风控集成)
- **阶段六：优化和测试** (持续进行)

### 待开始 📋
- **阶段五：归集和调度**

---

## 下一步行动

1. **完成提现模块收尾**
   - 实现风控集成 (3.3)
   - 添加提现状态管理 API (3.5)

2. **启动归集和调度开发 (阶段五)**
   - 实现归集服务
   - 实现资金调度服务

---

## 里程碑

- **M1**（2周）：基础架构完成，可以创建钱包和签名交易 ✅ **已完成**
- **M2**（4周）：充值功能完成，可以检测和入账 ✅ **已完成**
- **M3**（6周）：提现功能完成，可以完整提现流程 ✅ **已完成**
- **M4**（7周）：余额管理完成 ✅ **已完成**
- **M5**（9周）：归集和调度完成 ⏳ **待开始**
- **M6**（11周）：优化和测试完成 ⏳ **待开始**

---

**最后更新**：2025-11-24
**当前阶段**：阶段五 - 归集和调度
**完成度**：约 70%
