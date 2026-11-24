// Command gen generates standard Matter names from a pinned data model.
// The generated file is committed; normal builds do not need network access.
package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	modelVersion = "1.6.1"
	revision     = "3bcdd56ba54fb88b2afb4bfef575014671df7aa7" // v1.6.1.0
	baseURL      = "https://raw.githubusercontent.com/project-chip/connectedhomeip/" + revision + "/data_model/" + modelVersion
)

type member struct {
	ID        string `xml:"id,attr"`
	Name      string `xml:"name,attr"`
	Direction string `xml:"direction,attr"`
}

type clusterDocument struct {
	ID             string   `xml:"id,attr"`
	Name           string   `xml:"name,attr"`
	ClusterIDs     []member `xml:"clusterIds>clusterId"`
	Classification struct {
		BaseCluster string `xml:"baseCluster,attr"`
		Role        string `xml:"role,attr"`
	} `xml:"classification"`
	Attributes []member `xml:"attributes>attribute"`
	Events     []member `xml:"events>event"`
	Commands   []member `xml:"commands>command"`
}

type memberKey struct{ cluster, id uint32 }

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	sourceDir := flag.String("source-dir", "", "local data_model/<version> directory (avoids network access)")
	flag.Parse()
	if err := generate(*sourceDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(sourceDir string) error {
	var rawTypes map[string]string
	data, err := readSource(sourceDir, "device_types/device_type_ids.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &rawTypes); err != nil {
		return fmt.Errorf("decode device types: %w", err)
	}
	devices := make(map[uint32]string, len(rawTypes))
	for id, name := range rawTypes {
		number, err := parseID(id)
		if err != nil {
			return err
		}
		devices[number] = name
	}

	files, err := clusterFiles(sourceDir)
	if err != nil {
		return err
	}
	documents := make(map[string]*clusterDocument, len(files))
	bases := make(map[string]*clusterDocument, len(files))
	for _, file := range files {
		data, err := readSource(sourceDir, "clusters/"+file)
		if err != nil {
			return err
		}
		var document clusterDocument
		if err := xml.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode %s: %w", file, err)
		}
		documents[file] = &document
		for _, id := range document.ClusterIDs {
			if id.Name != "" {
				bases[id.Name] = &document
			}
		}
		if name := strings.TrimSuffix(document.Name, " Cluster"); name != "" {
			bases[name] = &document
		}
	}

	clusters := map[uint32]string{}
	applicationClusters := map[uint32]bool{}
	attributes := map[memberKey]string{}
	events := map[memberKey]string{}
	commands := map[memberKey]string{}
	for _, file := range files {
		document := documents[file]
		ids := document.ClusterIDs
		if len(ids) == 0 && document.ID != "" {
			ids = []member{{ID: document.ID, Name: strings.TrimSuffix(document.Name, " Cluster")}}
		}
		for _, id := range ids {
			if id.ID == "" {
				continue // Abstract base cluster.
			}
			clusterID, err := parseID(id.ID)
			if err != nil {
				return err
			}
			if err := addUnique(clusters, clusterID, identifier(id.Name), file); err != nil {
				return err
			}
			if document.Classification.Role == "application" {
				applicationClusters[clusterID] = true
			}
			for _, spec := range []struct {
				kind   string
				target map[memberKey]string
			}{{"attributes", attributes}, {"events", events}, {"commands", commands}} {
				members, err := resolveMembers(document, spec.kind, bases)
				if err != nil {
					return fmt.Errorf("%s: %w", file, err)
				}
				for memberID, name := range members {
					if err := addUnique(spec.target, memberKey{clusterID, memberID}, name, file); err != nil {
						return err
					}
				}
			}
		}
	}

	output, err := render(devices, clusters, applicationClusters, attributes, events, commands)
	if err != nil {
		return err
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("cannot locate generator source")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "catalog_generated.go")
	if err := os.WriteFile(path, output, 0o600); err != nil {
		return fmt.Errorf("write generated catalog: %w", err)
	}
	fmt.Printf("generated catalog: %d types, %d clusters, %d attributes, %d events, %d commands\n",
		len(devices), len(clusters), len(attributes), len(events), len(commands))
	return nil
}

func readURL(url string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "matterctl-catalog-generator")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

func readSource(sourceDir, name string) ([]byte, error) {
	if sourceDir == "" {
		return readURL(baseURL + "/" + name)
	}
	data, err := fs.ReadFile(os.DirFS(sourceDir), name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return data, nil
}

func clusterFiles(sourceDir string) ([]string, error) {
	var files []string
	if sourceDir != "" {
		entries, err := os.ReadDir(filepath.Join(sourceDir, "clusters"))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".xml") {
				files = append(files, entry.Name())
			}
		}
	} else {
		url := "https://api.github.com/repos/project-chip/connectedhomeip/contents/data_model/" +
			modelVersion + "/clusters?ref=" + revision
		data, err := readURL(url)
		if err != nil {
			return nil, err
		}
		var entries []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("decode cluster file list: %w", err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name, ".xml") {
				files = append(files, entry.Name)
			}
		}
	}
	slices.Sort(files)
	return files, nil
}

func resolveMembers(document *clusterDocument, kind string, bases map[string]*clusterDocument) (map[uint32]string, error) {
	result := map[uint32]string{}
	if parent := document.Classification.BaseCluster; parent != "" {
		base := bases[parent]
		if base == nil {
			return nil, fmt.Errorf("missing base cluster %q", parent)
		}
		inherited, err := resolveMembers(base, kind, bases)
		if err != nil {
			return nil, err
		}
		for id, name := range inherited {
			result[id] = name
		}
	}
	var current []member
	switch kind {
	case "attributes":
		current = document.Attributes
	case "events":
		current = document.Events
	case "commands":
		current = document.Commands
	default:
		return nil, fmt.Errorf("unknown member kind %q", kind)
	}
	for _, item := range current {
		if kind == "commands" && item.Direction != "commandToServer" {
			continue // Responses may reuse request IDs.
		}
		id, err := parseID(item.ID)
		if err != nil {
			return nil, err
		}
		result[id] = item.Name
	}
	return result, nil
}

func parseID(raw string) (uint32, error) {
	id, err := strconv.ParseUint(raw, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid Matter ID %q: %w", raw, err)
	}
	return uint32(id), nil
}

func identifier(name string) string {
	var out strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func addUnique[K comparable](target map[K]string, key K, name, source string) error {
	if existing, ok := target[key]; ok && existing != name {
		return fmt.Errorf("conflicting name for %v in %s: %q / %q", key, source, existing, name)
	}
	target[key] = name
	return nil
}

func render(devices, clusters map[uint32]string, applicationClusters map[uint32]bool, attributes, events, commands map[memberKey]string) ([]byte, error) {
	var out strings.Builder
	fmt.Fprintln(&out, "// Code generated by internal/matter/gen; DO NOT EDIT.")
	fmt.Fprintf(&out, "// Source: connectedhomeip data_model/%s at %s (Apache-2.0).\n", modelVersion, revision)
	fmt.Fprintln(&out, "package matter")
	fmt.Fprintln(&out)
	for _, table := range []struct {
		name    string
		entries map[uint32]string
	}{{"standardDeviceTypes", devices}, {"standardClusters", clusters}} {
		fmt.Fprintf(&out, "var %s = map[uint32]string{\n", table.name)
		ids := make([]uint32, 0, len(table.entries))
		for id := range table.entries {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			fmt.Fprintf(&out, "\t%d: %s,\n", id, strconv.Quote(table.entries[id]))
		}
		fmt.Fprintln(&out, "}")
		fmt.Fprintln(&out)
	}
	fmt.Fprintln(&out, "var standardApplicationClusters = map[uint32]bool{")
	applicationIDs := make([]uint32, 0, len(applicationClusters))
	for id := range applicationClusters {
		applicationIDs = append(applicationIDs, id)
	}
	slices.Sort(applicationIDs)
	for _, id := range applicationIDs {
		fmt.Fprintf(&out, "\t%d: true,\n", id)
	}
	fmt.Fprintln(&out, "}")
	fmt.Fprintln(&out)
	for _, table := range []struct {
		name    string
		entries map[memberKey]string
	}{{"standardAttributes", attributes}, {"standardEvents", events}, {"standardCommands", commands}} {
		fmt.Fprintf(&out, "var %s = map[memberKey]string{\n", table.name)
		keys := make([]memberKey, 0, len(table.entries))
		for key := range table.entries {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b memberKey) int {
			if a.cluster != b.cluster {
				return int(a.cluster) - int(b.cluster)
			}
			return int(a.id) - int(b.id)
		})
		for _, key := range keys {
			fmt.Fprintf(&out, "\t{%d, %d}: %s,\n", key.cluster, key.id, strconv.Quote(table.entries[key]))
		}
		fmt.Fprintln(&out, "}")
		fmt.Fprintln(&out)
	}
	formatted, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated catalog: %w", err)
	}
	return formatted, nil
}
