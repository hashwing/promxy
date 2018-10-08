package proxyconfig

import (
	"fmt"
	"io/ioutil"

	"github.com/ghodss/yaml"
)

// Group alert config
type Group struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

// Rule rule of alert
type Rule struct {
	Alert       string            `json:"alert"`
	Expr        string            `json:"expr"`
	For         string            `json:"for"`
	Labels      map[string]string `json:"labels"`
	Annotations Annotations       `json:"annotations"`
}

// Annotations another info
type Annotations struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// AddRule add alert rules
func (cfg *Config) AddRule(groups []Group) error {
	data, err := yaml.Marshal(groups)
	if err != nil {
		return err
	}
	if len(cfg.PromConfig.RuleFiles) > 0 {
		return ioutil.WriteFile(cfg.PromConfig.RuleFiles[0], data, 0666)
	}
	return fmt.Errorf("not found rule file")
}
