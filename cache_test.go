package reflect2

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestEffective(t *testing.T) {
	// CacheTestStructTypeCaller()
	Probe()
}

func TestGenerateCacheTypesForTest(t *testing.T) {
	count := 4096
	f, err := os.OpenFile("/Users/liucong/go/src/github.com/onionsheep/reflect2/cache_types_test.go",
		os.O_RDWR|os.O_CREATE, os.FileMode(0644))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	err = f.Truncate(0)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	sb := strings.Builder{}
	sb.WriteString(`package reflect2`)
	sb.WriteString("\n")
	sb.WriteString(`import (
	"fmt"
	"strings"
)`)
	sb.WriteString("\n")
	sb.WriteString("\n")
	for i := 0; i < count; i++ {
		sb.WriteString(`type CacheTestStructType`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(` struct{ FieldBool bool }`)
		sb.WriteString("\n")
	}
	sb.WriteString(`
func CacheTestStructTypeCaller() {
	sb := strings.Builder{}
`)
	for i := 0; i < count; i++ {
		sb.WriteString(`	sb.WriteString(fmt.Sprintf("%v", CacheTestStructType`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`{true}))`)
		sb.WriteString("\n")
	}
	sb.WriteString(`fmt.Printf("sb=%v", sb)
}`)
	sb.WriteString("\n")
	_, err = f.WriteString(sb.String())
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
}
