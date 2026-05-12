package main
import (
"os"
"text/template"
)
type Helper struct{}
func (h *Helper) AiEnabled() bool { return false }
func main() {
funcMap := template.FuncMap{"aiEnabled": (&Helper{}).AiEnabled}
tmpl, _ := template.New("test").Funcs(funcMap).Parse("{{ aiEnabled }}")
tmpl.Execute(os.Stdout, nil)
}
