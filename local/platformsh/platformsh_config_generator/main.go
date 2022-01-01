/*
 * Copyright (c) 2021-present Fabien Potencier <fabien@symfony.com>
 *
 * This file is part of Symfony CLI project
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/template"
)

type service struct {
	Type     string          `json:"type"`
	Runtime  bool            `json:"runtime"`
	Versions serviceVersions `json:"versions"`
}

type serviceVersions struct {
	Deprecated []string `json:"deprecated"`
	Supported  []string `json:"supported"`
}

var outputTemplate = template.Must(template.New("output").Parse(`// Code generated by platformsh_config_generator.
// DO NOT EDIT

/*
 * Copyright (c) 2021-present Fabien Potencier <fabien@symfony.com>
 *
 * This file is part of Symfony CLI project
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package platformsh

var availablePHPExts = map[string][]string{
{{ .Extensions -}}
}

var availableServices = []*service{
{{ .Services -}}
}
`))

func main() {
	extsAsString, err := parsePHPExtensions()
	if err != nil {
		panic(err)
	}

	servicesAsString, err := parseServices()
	if err != nil {
		panic(err)
	}

	data := map[string]interface{}{
		"Extensions": extsAsString,
		"Services":   servicesAsString,
	}
	var buf bytes.Buffer
	if err := outputTemplate.Execute(&buf, data); err != nil {
		panic(err)
	}
	f, err := os.Create("../local/platformsh/platformsh_config.go")
	if err != nil {
		panic(err)
	}
	f.Write(buf.Bytes())
}

func parseServices() (string, error) {
	resp, err := http.Get("https://raw.githubusercontent.com/platformsh/platformsh-docs/master/docs/data/registry.json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var services map[string]*service
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, &services); err != nil {
		return "", err
	}
	serviceNames := []string{}
	for name := range services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)
	servicesAsString := ""
	for _, name := range serviceNames {
		s := services[name]
		if !s.Runtime {
			servicesAsString += "\t{\n"
			servicesAsString += fmt.Sprintf("\t\tType: \"%s\",\n", s.Type)
			servicesAsString += "\t\tVersions: serviceVersions{\n"
			if len(s.Versions.Deprecated) > 0 {
				servicesAsString += fmt.Sprintf("\t\t\tDeprecated: []string{\"%s\"},\n", strings.Join(s.Versions.Deprecated, "\", \""))
			} else {
				servicesAsString += "\t\t\tDeprecated: []string{},\n"
			}
			if len(s.Versions.Supported) > 0 {
				servicesAsString += fmt.Sprintf("\t\t\tSupported:  []string{\"%s\"},\n", strings.Join(s.Versions.Supported, "\", \""))
			} else {
				servicesAsString += "\t\t\tSupported: []string{},\n"
			}
			servicesAsString += "\t\t},\n"
			servicesAsString += "\t},\n"
		}
	}
	return servicesAsString, nil
}

func parsePHPExtensions() (string, error) {
	resp, err := http.Get("https://raw.githubusercontent.com/platformsh/platformsh-docs/master/docs/src/languages/php/extensions.md")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var versions []string
	orderedExtensionNames := []string{}
	extensions := make(map[string][]string)
	started := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if started {
			if strings.HasPrefix(line, "| ---") {
				continue
			}
			if !strings.HasPrefix(line, "| ") {
				break
			}
			name, available := parseLine(line)
			name = strings.ToLower(strings.Trim(name, "`"))
			if _, ok := extensions[name]; ok {
				log.Printf("WARNING: The %s extension is listed twice, ignoring extra definition!\n", name)
			} else {
				orderedExtensionNames = append(orderedExtensionNames, name)
				var vs []string
				for i, v := range available {
					if v != "" {
						vs = append(vs, versions[i])
					}
				}
				extensions[name] = vs
			}
		}
		if strings.HasPrefix(line, "| Extension") {
			started = true
			_, versions = parseLine(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	maxNameLen := 0
	for name := range extensions {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}
	extsAsString := ""

	for _, name := range orderedExtensionNames {
		versions = extensions[name]
		line := fmt.Sprintf("\t\"%s\":%s{", name, strings.Repeat(" ", maxNameLen-len(name)+1))
		for i, version := range versions {
			line = line + fmt.Sprintf(`"%s"`, version)
			if i != len(versions)-1 {
				line = line + ", "
			}
		}
		line = line + "},"
		extsAsString = extsAsString + line + "\n"
	}
	return extsAsString, nil
}

func parseLine(line string) (string, []string) {
	next := strings.Index(line[1:], "|") + 1
	name := strings.TrimSpace(line[1:next])
	var versions []string
	for {
		current := next + 1
		nextIndex := strings.Index(line[current:], "|")
		if nextIndex == -1 {
			break
		}
		next = nextIndex + current
		versions = append(versions, strings.TrimSpace(line[current:next]))
		if next >= len(line) {
			break
		}
	}
	return name, versions
}
