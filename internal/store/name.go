package store

import "strings"

// envAliases maps environment words found in object names to one canonical name.
var envAliases = map[string]string{
	"dev": "dev", "develop": "dev", "development": "dev",
	"stg": "staging", "stage": "staging", "staging": "staging",
	"prod": "prod", "prd": "prod", "production": "prod", "live": "prod",
	"qa": "qa", "uat": "uat", "test": "test", "testing": "test",
	"preprod": "preprod", "nonprod": "nonprod", "non-prod": "nonprod", "np": "nonprod", "sandbox": "sandbox", "sbx": "sandbox",
	"demo": "demo", "perf": "perf", "beta": "beta", "canary": "canary", "local": "local",
}

// EnvOrder sorts environments the way people read them, not alphabetically.
var EnvOrder = map[string]int{"local": 0, "dev": 1, "test": 2, "qa": 3, "uat": 4, "sandbox": 5, "demo": 6, "perf": 7, "staging": 8, "nonprod": 9, "beta": 10, "preprod": 11, "canary": 12, "prod": 13}

// SplitName turns an object name such as "payments-api-prod-env",
// "payments-api/production.env" or "auth_staging" into service and environment.
// When no environment word is found, env is empty and service is the full name.
func SplitName(object string) (service, env string) {
	name := strings.ToLower(object)
	for _, suf := range []string{".env", "-env", "_env", "/env"} {
		if strings.HasSuffix(name, suf) && len(name) > len(suf) {
			name = strings.TrimSuffix(name, suf)
			break
		}
	}
	tokens := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '/' || r == '.' })
	for i := len(tokens) - 1; i >= 0; i-- {
		if canon, ok := envAliases[tokens[i]]; ok {
			rest := append(append([]string{}, tokens[:i]...), tokens[i+1:]...)
			if len(rest) == 0 {
				return object, canon
			}
			return strings.Join(rest, "-"), canon
		}
	}
	return object, ""
}
