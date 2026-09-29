package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/swxs/skill-manager/internal/search"
)

func cmdSearch(_ string, args []string) int {
	query, code := parseSearch(args)
	if code != 0 {
		return code
	}
	return searchList(query, search.Current())
}

func parseSearch(args []string) (string, int) {
	var words []string
	for _, arg := range args {
		if arg == "--install" {
			fmt.Fprintln(os.Stderr, "不再接受 search --install")
			return "", 2
		}
		if stringsHasFlag(arg) {
			fmt.Fprintf(os.Stderr, "无法识别的参数: %s\n", arg)
			return "", 2
		}
		words = append(words, arg)
	}
	return strings.Join(words, " "), 0
}

func stringsHasFlag(arg string) bool {
	return len(arg) > 0 && arg[0] == '-'
}

func searchList(query string, cfg search.Config) int {
	out := search.DefaultPipeline(cfg).List(query)
	switch out.Kind {
	case search.KindHits:
		if err := search.Remember(out.Records); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
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
