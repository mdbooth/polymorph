package templates

import (
	"bytes"
	"text/template"
)

func ExpandTemplate(templateString string, params map[string]string) (string, error) {
	t, err := template.New("").Parse(templateString)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
