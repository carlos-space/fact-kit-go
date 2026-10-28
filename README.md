# fact-kit Go 讀取套件

讀取 fact-kit 套件在建置時產生的 `index.json`，回傳某個讀者看得到的 facts。選列、版號與解析的規則和 JS 版相同，共用的規格與測試資料放在倉庫的 `conformance/`。

## 安裝

```sh
go get github.com/carlos-space/fact-kit-go@vX.Y.Z
```

模組路徑是 `github.com/carlos-space/fact-kit-go`，package 名是 `factkit`。需要 Go 1.26 以上。本倉庫發版的 tag 是 `fact-kit-go/vX.Y.Z`。Go 工具鏈從鏡像倉庫 `github.com/carlos-space/fact-kit-go` 取模組，那裡的 tag 是 `vX.Y.Z`，所以 `@vX.Y.Z` 對應鏡像倉庫的 tag `vX.Y.Z`。

## 用法

```go
import factkit "github.com/carlos-space/fact-kit-go"

reader, err := factkit.ReadFacts("some-facts/sdk", factkit.ReadFactsOptions{
	Root:    "/path/to/hub",   // 省略時用目前的工作目錄
	Version: new("0.50.0"),    // nil 表示最新已發布，"head" 表示還沒發布的狀態
	Include: []string{"label"}, // head 讀者額外要讀的 pending 標籤
})
if err != nil {
	var e *factkit.FactReadError
	if errors.As(err, &e) {
		fmt.Println(e.Code) // 例如 factkit.CodeNotFound
	}
	return err
}

limit, err := reader.Value("demo.limit") // content.value，沒有這筆 fact 時回傳 error
```

- `ReadFacts(spec, opts)` 從 `opts.Root` 往上找 `node_modules`，讀 `<套件>` 或 `<套件>/<路徑>` 的 `index.json`。`Root` 省略時用目前的工作目錄。
- `SelectFacts(index []byte, opts SelectOptions)` 是純函式，輸入 `index.json` 的位元組，不碰檔案系統。
- `FactReader` 有 `Package`、`Version`、`Entry`、`Describes`、`Publish`（`*Publish`，有 `Target` 與 `Name`，index 沒有 `publish` 時是 `nil`，不用來比對版本）、`Facts`，以及 `Has`、`Get`、`Value`。
- `Version` 是 `*string`，因為空字串是無效版號，和「沒給版號」不同。Go 1.26 可以寫 `new("head")`。`Platform` 與 `OS` 是字串，空字串表示沒給。
- `CompareVersions(a, b)` 與 `ReleaseLine(version)` 提供版號比較與發布線，版號必須是完整的 `X.Y.Z`。

### 套件解析

照 Node 的 `require` 規則，從 `Root` 開始：

1. self-reference：最近的 `package.json` 名稱等於被讀的套件，而且有 `exports` 時用它解析。
2. 往上層找第一個有 `node_modules/<套件>` 的目錄，可以是符號連結。
3. `exports` 支援字串、陣列、條件物件（`node-addons`、`node`、`require`、`module-sync`、`default`，照 key 的順序）、巢狀條件、`*` 與 `null`。
4. 沒有 `exports` 時退回檔案路徑。檔案不存在時，往上一層的 `node_modules` 繼續找。

`NODE_PATH` 與全域資料夾不實作，這是和 Node 的已知差異。

## 錯誤代碼

錯誤是 `*FactReadError`，用 `errors.As` 取得，`Code` 是下列常數。

| 常數                         | `Code` 的值                | 何時回傳                                                                        |
| :--------------------------- | :------------------------- | :------------------------------------------------------------------------------ |
| `CodeNotAnIndex`             | `not-an-index`             | 檔案不是 JSON，頂層不是物件，`facts` 不是陣列，或 `package` 不是字串            |
| `CodeUnsupportedIndexFormat` | `unsupported-index-format` | `indexFormat` 的 major 不是 1，或不是 `<數字>.<數字>` 的字串                    |
| `CodeNotFound`               | `not-found`                | 找不到套件、套件沒有 export 那個 `index.json`、檔案不存在，或 `spec` 寫法不合格 |
| `CodeInvalidVersion`         | `invalid-version`          | 查詢的 `Version` 不是 `"head"` 也不是 semver，或 index 裡的 `since` 不是版號    |

## 和 JS 版不同的地方

- 不比對 `describes` 和已安裝的產品版本，所以不會因為產品比 facts 新而報錯，也不會自動挑版本。要讀特定版本時自己傳 `Version`。
- 不提供 `onRead`。
- `Get` 與 `Value` 找不到 fact 時回傳一般的 `error`，不是 `*FactReadError`。

## 測試

```sh
go test -v ./...
```

測試讀倉庫的 `conformance/`，先看環境變數 `FACT_KIT_CONFORMANCE`，沒有就從測試檔位置往上找。新增案例只要放進 `conformance/`，不用改程式。
