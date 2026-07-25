# CLIProxyAPI Plugin Lockdown

[English](README.md) | [中文](README_CN.md) | 日本語

CLIProxyAPI をベースにした**非公式のセキュリティ強化 fork**です。プラグインを必要としないデプロイ向けに、プロセス単位のプラグインロックダウンポリシーを追加し、未認証のルートパスから得られる製品フィンガープリントを減らします。

この fork は、上流の OpenAI、Gemini、Claude、Codex などの互換 API、OAuth、複数アカウント運用、ラウンドロビン負荷分散を維持します。本書は特定のモデル、サブスクリプション、第三者の中継サービスを推奨しません。

> [!IMPORTANT]
> これは CLIProxyAPI の公式リリースではなく、上流メンテナーを代表するものでもありません。問題を報告する際は、この fork を使用していることと、基準バージョンおよびコミット情報を明記してください。

## 目次

- [バージョンと由来](#バージョンと由来)
- [セキュリティ目標](#セキュリティ目標)
- [プラグインロックダウンポリシー](#プラグインロックダウンポリシー)
- [API の動作](#api-の動作)
- [ルートページとセキュリティヘッダー](#ルートページとセキュリティヘッダー)
- [セキュリティ境界](#セキュリティ境界)
- [ソースからのビルド](#ソースからのビルド)
- [ローカル実行](#ローカル実行)
- [Docker のローカルビルドと実行](#docker-のローカルビルドと実行)
- [Compose のリスク](#compose-のリスク)
- [ヘルスチェックとポリシー検証](#ヘルスチェックとポリシー検証)
- [設定検証用一時ファイル](#設定検証用一時ファイル)
- [上流との同期](#上流との同期)
- [ドキュメントと開発](#ドキュメントと開発)
- [ライセンスと帰属](#ライセンスと帰属)

## バージョンと由来

| 項目 | 値 |
| --- | --- |
| 上流プロジェクト | [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) |
| 上流基準バージョン | `v7.2.80` |
| 基準コミット | `09da52ad509e2c18e7b9540db3b98c2214c280aa` |
| プラグインロックダウンコミット | `c8b16d049a9d5e546ebc2e6267de8970c82aeca3` |
| ルート偽装・一時ファイルコミット | `c20f6e4b5a2b012635464077c50f2eb82ce0e0e8` |
| ライセンス | MIT |

fork の 2 つのコミットは次の役割を持ちます。

- `c8b16d04`：`CLIPROXY_DISABLE_PLUGINS` プロセスポリシーを追加し、標準 CLI 起動経路でプラグイン設定を正規化し、プラグインストアとプラグイン変更 API を制限します。
- `c20f6e4b`：`GET /` を汎用 Welcome HTML に変更し、セキュリティヘッダーを追加し、Management API の設定検証ファイルをシステム一時ディレクトリへ移します。

## セキュリティ目標

この fork は次のようなデプロイを想定しています。

- 必要なのはコアプロキシ、OAuth、互換 API、複数アカウントのスケジューリングのみである。
- 運用ポリシーにより、プロセス起動時からプラグインを強制的に無効化する必要がある。
- 管理者は既存プラグインの状態確認、設定読み取り、レガシープラグイン削除を引き続き行う必要がある。
- 未認証のルートアクセスに製品名や API ルート一覧を返したくない。

これは**多層防御と誤設定対策**を提供します。完全なサンドボックスでも、すべての拡張経路を対象とする形式的な安全証明でもありません。

## プラグインロックダウンポリシー

### プロセス起動前に変数を注入する

安全に起動するには、プログラム実行前の実プロセス環境に次の値が必要です。

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

プロセスマネージャーから直接注入することもできます。

- systemd の `Environment=` または `EnvironmentFile=`。
- Docker の `-e`、`--env-file`、Compose の `environment`。
- Kubernetes、Nomad、その他オーケストレーターのコンテナ環境変数。
- `exec` 前に環境変数をエクスポートする CI/CD、起動スクリプト、ホストのサービスマネージャー。

> [!WARNING]
> **作業ディレクトリの `.env` にだけ `CLIPROXY_DISABLE_PLUGINS=true` を書く方法を、安全な起動方法として扱わないでください。**
>
> 標準サーバーエントリは、作業ディレクトリの `.env` を読み込む前に設定を読み取り、プラグイン CLI フラグ登録を含むプラグイン bootstrap を実行します。そのため `.env` だけで渡した値は遅すぎ、bootstrap 段階がロックダウンポリシーで保護されたことを保証できません。

環境ファイルを使う場合は、シェル、systemd、Docker、またはオーケストレーターで実行ファイルの起動前に読み込んでください。例：

```bash
set -a
. /secure/path/cliproxy.env
set +a
exec ./cli-proxy-api --config ./config.yaml
```

### 標準 CLI 経路での動作

実プロセス環境の `CLIPROXY_DISABLE_PLUGINS` が Go の `strconv.ParseBool` で真として解析される場合：

- 設定正規化時に `plugins.enabled` が `false` に強制されます。
- すべての `plugins.configs.<id>.enabled` が `false` に強制されます。
- 各プラグインの生設定ノード内の `enabled` も `false` に変更されます。
- プラグイン機能サポートフラグは `0` を返します。
- プラグインストアへのアクセス、インストール、プラグイン設定変更はポリシーにより拒否されます。
- 完全 YAML 設定 API からグローバルまたは個別プラグインを再有効化する要求は拒否されます。

明示的に次の値を使用することを推奨します。

```text
CLIPROXY_DISABLE_PLUGINS=true
```

変数が未設定、空、または真のブール値として解析できない場合、ロックダウンポリシーは有効になりません。

### 一部のプラグイン API を残す理由

ロックダウンは、すべてのプラグインコードや管理ルートを削除するものではありません。次の読み取り専用またはクリーンアップ機能を残します。

- 検出済みまたは登録済みプラグインの一覧表示。
- 個別プラグイン設定の読み取り。
- 構造化された完全設定または生の `config.yaml` の読み取り。
- 監査後のクリーンアップや廃止のための既存プラグイン削除。

これにより、プラグインの追加、インストール、有効化、変更を禁止しながら、棚卸しと削除の経路を維持します。

## API の動作

Management API のプレフィックスは `/v0/management` です。リモートから利用できるかどうかは、引き続き上流の管理設定と管理キーにより制御されます。

### ロックダウン時の統一 403 応答

ポリシー有効時、次の操作は HTTP `403 Forbidden` を返します。

| メソッド | パス | 動作 |
| --- | --- | --- |
| `GET` | `/v0/management/plugin-store` | プラグインストアへのアクセスを拒否 |
| `POST` | `/v0/management/plugin-store/:id/install` | プラグインのインストールを拒否 |
| `PATCH` | `/v0/management/plugins/:id/enabled` | プラグイン有効状態の変更を拒否 |
| `PUT` | `/v0/management/plugins/:id/config` | プラグイン設定の置換を拒否 |
| `PATCH` | `/v0/management/plugins/:id/config` | プラグイン設定の変更を拒否 |
| `PUT` | `/v0/management/config.yaml` | グローバルまたは個別プラグインを有効化する YAML を拒否 |

統一 JSON 応答：

```json
{
  "error": "plugin_capability_disabled",
  "message": "plugin capability is disabled by server policy"
}
```

### 監査とクリーンアップのために維持される API

この fork のプラグインロックダウン処理は、次の管理操作を無効化しません。

| メソッド | パス | 用途 |
| --- | --- | --- |
| `GET` | `/v0/management/plugins` | プラグイン棚卸し |
| `GET` | `/v0/management/plugins/:id/config` | 設定監査 |
| `DELETE` | `/v0/management/plugins/:id` | 削除とクリーンアップ |
| `GET` | `/v0/management/config` | 構造化設定の読み取り |
| `GET` | `/v0/management/config.yaml` | 生設定の読み取り |

これらの API にも通常の管理認証とアクセス制御が適用されます。

## ルートページとセキュリティヘッダー

未認証の `GET /` は最小限の HTML を返します。

```html
<h1>Welcome</h1>
```

上流のルート JSON に含まれていた製品名や API パス例は返しません。応答には次のヘッダーが含まれます。

- `Cache-Control: no-store`
- `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'`
- `Referrer-Policy: no-referrer`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`

ルートハンドラーは自身の応答にある一般的な CORS ヘッダーもクリアします。この変更によってコアの `/v1`、`/healthz`、Management API 全体が無効になるわけではありません。

## セキュリティ境界

この fork は次の制約を前提に評価してください。

- プラグインコードは**バイナリから削除されていません**。
- ロックダウンは**デフォルトで有効ではありません**。環境変数を明示的に渡す必要があります。
- すべての管理 API が閉じられるわけではありません。監査、読み取り、削除の経路は残ります。
- 汎用ルートページは完全なフィンガープリント除去ではありません。ポート、TLS、通信特性、他のルート、エラー応答から実装を推測できる場合があります。
- すべての SDK 組み込み、カスタムエントリポイント、変更済みビルド、悪意あるローカルホスト環境で回避不能だとは主張しません。
- プロセス環境、実行ファイル、起動引数、設定マウント、コンテナ定義を変更できる主体は、信頼されたデプロイ制御面に属します。
- Management API、設定ファイル、認証ディレクトリ、コンテナランタイムには、引き続き最小権限、ネットワーク分離、適切な秘密管理が必要です。

## ソースからのビルド

要件：

- Go `1.26` 以降。
- Git。
- 同名の外部イメージではなく、この fork の現在のソース。

```bash
git clone https://github.com/86669666/CLIProxyAPI-plugin-lockdown.git
cd CLIProxyAPI-plugin-lockdown
git rev-parse --short HEAD
go build -o cli-proxy-api ./cmd/server
```

Go コードを変更した場合、リポジトリでは次を実行します。

```bash
gofmt -w .
go build -o test-output ./cmd/server && rm test-output
go test ./...
```

README のみの変更では Go ファイルの再フォーマットは不要です。

## ローカル実行

テンプレートからローカル設定を作成し、API キー、OAuth 認証情報、認証ディレクトリ、管理アクセスは上流ドキュメントに従って設定してください。

```bash
cp config.example.yaml config.yaml
```

ロックダウンを有効にして起動します。

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

上流の主なフラグ：

- `--config <path>`：設定ファイルを指定します。
- `--tui`：ターミナル UI を起動します。
- `--standalone`：TUI をスタンドアロンモードで実行します。
- `--local-model`：組み込みモデルカタログのみを使用し、リモート更新をスキップします。
- `--no-browser`：OAuth 中にブラウザーを自動起動しません。
- `--oauth-callback-port <port>`：OAuth コールバックポートを指定します。

実際の API キー、管理キー、OAuth トークンを、ソース管理対象のコマンド、README、チケット、ログへ貼り付けないでください。

## Docker のローカルビルドと実行

この fork のソースを確実に使用するため、ローカルでイメージを明示的にビルドします。

```bash
docker build \
  --build-arg VERSION=v7.2.80-lockdown \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t cli-proxy-api-lockdown:local .
```

実行例：

```bash
docker run --rm \
  --name cli-proxy-api-lockdown \
  -e CLIPROXY_DISABLE_PLUGINS=true \
  -p 8317:8317 \
  -v "$(pwd)/config.yaml:/CLIProxyAPI/config.yaml:ro" \
  -v "$(pwd)/auths:/root/.cli-proxy-api" \
  cli-proxy-api-lockdown:local
```

Management API から設定を書き込む必要がある場合、`config.yaml` を読み取り専用でマウントしないでください。保護された書き込み可能な永続ボリュームを使用し、ホスト側権限を制限してください。

## Compose のリスク

現在の `docker-compose.yml` と `docker-compose.cluster.yml` には次のデフォルトがあります。

```yaml
image: ${CLI_PROXY_IMAGE:-eceasy/cli-proxy-api:latest}
pull_policy: always
```

したがって通常の `docker compose up` は、この fork のローカルソースからビルドしたイメージではなく、外部の `latest` イメージを pull して実行する可能性があります。

また、既存の Compose ファイルはデフォルトで `CLIPROXY_DISABLE_PLUGINS=true` をコンテナへ渡しません。デフォルトの Compose 起動をロックダウン済みデプロイと見なしてはいけません。

安全に使用するには、次をすべて行ってください。

1. ローカルビルドしたタグ、または検証済み fork イメージの digest を使用する。
2. `pull_policy: always` を削除または上書きし、外部イメージによる置換を防ぐ。
3. コンテナ作成時に `CLIPROXY_DISABLE_PLUGINS=true` を明示的に注入する。
4. `docker compose config` で展開後の設定を確認する。
5. 起動後にヘルスチェックとポリシー検証を実行する。

リポジトリの Compose ファイルを変更せず、デプロイ専用の上書きファイルを利用できます。

```yaml
services:
  cli-proxy-api:
    image: cli-proxy-api-lockdown:local
    pull_policy: never
    environment:
      CLIPROXY_DISABLE_PLUGINS: "true"
```

```bash
docker compose -f docker-compose.yml -f compose.lockdown.yml config
docker compose -f docker-compose.yml -f compose.lockdown.yml up -d
```

## ヘルスチェックとポリシー検証

### ヘルスチェック

```bash
curl --fail --silent --show-error http://127.0.0.1:8317/healthz
```

期待する応答：

```json
{"status":"ok"}
```

### ルートページの確認

```bash
curl --include http://127.0.0.1:8317/
```

ステータス `200`、`text/html` の Content-Type、汎用 Welcome 内容のみであること、前述のセキュリティヘッダーが存在することを確認します。

### プラグインポリシーの確認

ローカルシェルで管理キーを安全に設定します。

```bash
export MANAGEMENT_KEY='<set-locally>'
```

制限対象 API を呼び出します。

```bash
curl --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugin-store
```

期待する HTTP ステータスは `403` で、`error` フィールドは `plugin_capability_disabled` です。

維持されている棚卸し API を確認します。

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugins
```

プロキシ API の検証例：

```bash
export API_KEY='<set-locally>'
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${API_KEY}" \
  http://127.0.0.1:8317/v1/models
```

すべての `Authorization` 例では `${MANAGEMENT_KEY}` または `${API_KEY}` のみを使用しています。リポジトリへコミットされる実値に置き換えないでください。

## 設定検証用一時ファイル

Management API が `PUT /v0/management/config.yaml` を受け取ると、一時ファイルへ書き込み、設定ローダーを呼び出して内容を検証します。

この fork は次を使用します。

```text
os.CreateTemp("", "cliproxyapi-config-validate-*.yaml")
```

空のディレクトリ引数により、Go は実際の `config.yaml` と同じ場所ではなく、システム一時ディレクトリを使用します。成功時も失敗時も、ハンドラーは一時ファイルの削除を試みます。

この変更は、設定ディレクトリに短時間の検証コピーが現れる可能性を減らします。ただし、システム一時ディレクトリの適切な権限、ディスク暗号化、ホスト分離の代わりにはなりません。

## 上流との同期

この fork の文書化された基準は `v7.2.80` / `09da52ad` です。上流変更をマージまたは rebase するたびに、セキュリティ前提を再監査してください。

最低限、次を確認します。

- `cmd/server/main.go` のプラグイン bootstrap と `.env` 読み込み順序。
- `internal/config` のプラグイン設定解析と正規化。
- `internal/pluginhost` のプラグイン読み込み、機能表示、動的ルート登録。
- `/v0/management` 配下のプラグインストア、有効化、設定、削除、完全 YAML 書き込み API。
- `GET /`、CORS ミドルウェア、セキュリティヘッダー。
- Compose のデフォルトイメージ、pull ポリシー、環境変数伝播。
- 標準 CLI 起動経路を通らない新しいエントリポイントや SDK 組み込み方式。

同期後、最低限次を実行します。

```bash
gofmt -w .
go test ./...
go build -o test-output ./cmd/server && rm test-output
git diff --check
```

パッチが衝突なく適用できただけで、新しい上流実行経路もポリシー対象だと判断しないでください。

## ドキュメントと開発

- 設定テンプレート：[`config.example.yaml`](config.example.yaml)
- SDK 使用方法：[`docs/sdk-usage.md`](docs/sdk-usage.md)
- SDK 高度な使用方法：[`docs/sdk-advanced.md`](docs/sdk-advanced.md)
- SDK アクセス制御：[`docs/sdk-access.md`](docs/sdk-access.md)
- 認証情報の読み込みと更新：[`docs/sdk-watcher.md`](docs/sdk-watcher.md)
- カスタム Provider 例：[`examples/custom-provider`](examples/custom-provider)

セキュリティパッチは小さく検証可能に保ち、キーやトークンをログへ出力せず、ポリシー境界に焦点を当てたテストを追加してください。

## ライセンスと帰属

このリポジトリは引き続き [MIT License](LICENSE) で提供されます。

CLIProxyAPI の原設計、主要実装、上流保守は [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) とそのコントリビューターに帰属します。この fork は `v7.2.80` 基準の上に、本書で説明した非公式のセキュリティ変更のみを維持します。

利用、変更、再配布の際は、MIT ライセンス本文と既存の著作権表示を保持してください。
