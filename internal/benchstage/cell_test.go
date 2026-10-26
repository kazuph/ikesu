//go:build benchstage

package benchstage

import (
	"encoding/json"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"reflect"
	"testing"
)

func TestCellLayout(t *testing.T) {
	typ := reflect.TypeOf(uv.Cell{})
	fields := []map[string]any{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fields = append(fields, map[string]any{"name": f.Name, "type": f.Type.String(), "offset": f.Offset, "size": f.Type.Size()})
	}
	data, err := json.Marshal(map[string]any{"type": typ.String(), "size": typ.Size(), "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(data))
}
