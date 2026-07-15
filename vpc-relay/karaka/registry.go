package karaka

import (
	"fmt"
	"sort"
	"sync"
)

// ParamSpec décrit un paramètre d'outil pour le LLM.
type ParamSpec struct {
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
}

// Tool est un outil enregistré que les kāraka peuvent appeler.
type Tool struct {
	ID          string               `json:"id"`
	Description string               `json:"description"`
	Category    string               `json:"category"` // browser, sandbox, sms, vpc
	Params      map[string]ParamSpec `json:"params"`
	Returns     string               `json:"returns"`
	Handler     func(params map[string]interface{}) (interface{}, error) `json:"-"`
}

// Karaka décrit un acteur enregistré dans le système.
// Un kāraka est une émanation d'identité qui accomplit des tâches
// avec des outils, sous l'autorité du nœud personnel.
type Karaka struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Tier         string            `json:"tier"` // L1, L2, L3
	Status       string            `json:"status"` // idle, busy, sleeping, offline
	Capabilities []string          `json:"capabilities"`
	Tools        map[string]string `json:"tools"` // tool_id ou category.* → "allow"|"ask"|"deny"
	MaxSteps     int               `json:"max_steps"`
}

var (
	mu         sync.RWMutex
	tools      = map[string]Tool{}
	karakaList = map[string]Karaka{}
)

// RegisterTool ajoute un outil au registre.
func RegisterTool(t Tool) {
	mu.Lock()
	defer mu.Unlock()
	tools[t.ID] = t
}

// GetTool retourne un outil par son ID.
func GetTool(id string) (Tool, bool) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := tools[id]
	return t, ok
}

// ListTools retourne tous les outils triés par ID.
func ListTools() []Tool {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ExecuteTool appelle un outil par ID avec des paramètres.
func ExecuteTool(id string, params map[string]interface{}) (interface{}, error) {
	t, ok := GetTool(id)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", id)
	}
	if t.Handler == nil {
		return nil, fmt.Errorf("tool %s has no handler", id)
	}
	if err := ValidateParams(t, params); err != nil {
		return nil, err
	}
	return t.Handler(params)
}

// RegisterKaraka ajoute un kāraka au registre.
func RegisterKaraka(k Karaka) {
	mu.Lock()
	defer mu.Unlock()
	karakaList[k.ID] = k
}

// ListKarakas retourne tous les kāraka.
func ListKarakas() []Karaka {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Karaka, 0, len(karakaList))
	for _, k := range karakaList {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CheckPermission vérifie si un kāraka peut utiliser un outil.
// Retourne: "allow", "ask", "deny".
func CheckPermission(karakaID, toolID string) string {
	mu.RLock()
	defer mu.RUnlock()
	k, ok := karakaList[karakaID]
	if !ok {
		return "deny"
	}
	perm, ok := k.Tools[toolID]
	if !ok {
		category := ""
		if t, ok := tools[toolID]; ok {
			category = t.Category
		}
		perm, ok = k.Tools[category+".*"]
		if !ok {
			return "deny"
		}
	}
	return perm
}

// ValidateParams vérifie que tous les params required sont présents.
func ValidateParams(tool Tool, params map[string]interface{}) error {
	for pname, spec := range tool.Params {
		if spec.Required {
			if _, exists := params[pname]; !exists {
				return fmt.Errorf("missing required param: %s", pname)
			}
		}
	}
	return nil
}
