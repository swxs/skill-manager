package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/swxs/skill-manager/internal/gitpack"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/search"
)

var searchInstallFunc = defaultSearchInstall

func defaultSearchInstall(library, url string) int {
	return installRemote(library, url, "", false)
}

// SetSearchInstallForTest 替换点名后的安装步骤。传入 nil 恢复现有 install。
func SetSearchInstallForTest(fn func(library, url string) int) {
	if fn == nil {
		searchInstallFunc = defaultSearchInstall
		return
	}
	searchInstallFunc = fn
}

func cmdSearch(library string, args []string) int {
	token, query, code := parseSearch(args)
	if code != 0 {
		return code
	}
	cfg := search.Current()
	if token != "" {
		return searchInstallNamed(library, token, cfg)
	}
	return searchList(query, cfg)
}

func parseSearch(args []string) (string, string, int) {
	var words []string
	token := ""
	installing := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--install" {
			if i+1 >= len(args) || stringsHasFlag(args[i+1]) {
				fmt.Fprintln(os.Stderr, "缺少 --install 的值")
				return "", "", 2
			}
			if installing {
				fmt.Fprintln(os.Stderr, "search --install 只接受一个点号串")
				return "", "", 2
			}
			installing = true
			token = args[i+1]
			i++
			continue
		}
		if stringsHasFlag(arg) {
			fmt.Fprintf(os.Stderr, "无法识别的参数: %s\n", arg)
			return "", "", 2
		}
		words = append(words, arg)
	}
	if installing {
		if len(words) > 0 {
			fmt.Fprintln(os.Stderr, "search --install 只接受一个点号串")
			return "", "", 2
		}
		return token, "", 0
	}
	return "", strings.Join(words, " "), 0
}

func stringsHasFlag(arg string) bool {
	return len(arg) > 0 && arg[0] == '-'
}

func searchList(query string, cfg search.Config) int {
	out := search.DefaultPipeline(cfg).List(query)
	switch out.Kind {
	case search.KindHits:
		fmt.Print(search.FormatList(out.Records))
		return 0
	case search.KindEmpty:
		return 0
	case search.KindTooShort:
		fmt.Fprintln(os.Stderr, "检索词过短")
		return 2
	default:
		fmt.Fprintln(os.Stderr, "收集站不可用")
		return 1
	}
}

func searchInstallNamed(library, token string, cfg search.Config) int {
	catalog, id, ok := search.SplitToken(token)
	if !ok {
		fmt.Fprintf(os.Stderr, "点号串无法切开: %s\n", token)
		return 2
	}
	cat, ok := search.New(catalog, cfg)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知收集站: %s\n", catalog)
		return 2
	}
	rec, ok := search.Exact(cat.Search(id), id)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这条收录: %s\n", token)
		return 1
	}
	root := search.RepoRoot(rec.RawURL)
	if root == "" {
		fmt.Printf("未安装，没有 Git 地址: %s\n", token)
		return 0
	}
	packName, err := gitpack.NameFromURL(root)
	if err != nil {
		fmt.Println(err.Error())
		return 2
	}
	dest := filepath.Join(library, packName)
	if lib.Exists(dest) {
		if gitpack.Root(dest) != "" {
			fmt.Printf("库里已有 git 包: %s\n", packName)
		} else {
			fmt.Printf("库里已有非 git 目录: %s\n", packName)
		}
		return 0
	}
	return searchInstallFunc(library, root)
}
