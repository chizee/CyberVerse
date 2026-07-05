package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var yamlMu sync.Mutex

var conventionalModelConfigCategories = []struct {
	category string
	dirName  string
}{
	{category: "omni", dirName: "omni_models"},
	{category: "llm", dirName: "llm_models"},
	{category: "embedding", dirName: "embedding_models"},
	{category: "tts", dirName: "tts_models"},
	{category: "asr", dirName: "asr_models"},
}

// ReadYAMLNode reads a YAML file into a yaml.Node tree without expanding env vars.
func ReadYAMLNode(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// yaml.Unmarshal wraps in a DocumentNode; return the inner mapping.
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return &doc, nil
	}
	return &doc, nil
}

// ReadResolvedYAMLNode reads the main config and merges external model configs
// for read-only consumers.
func ReadResolvedYAMLNode(path string) (*yaml.Node, error) {
	doc, err := ReadYAMLNode(path)
	if err != nil {
		return nil, err
	}
	if err := MergeAvatarModelConfigDir(doc, path); err != nil {
		return nil, err
	}
	if err := MergeConventionalModelConfigDirs(doc, path); err != nil {
		return nil, err
	}
	return doc, nil
}

// mappingRoot returns the top-level mapping node from a document node.
func mappingRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

// GetNodeAtPath traverses a yaml.Node tree by dot-separated path.
// An empty dotPath returns the root mapping node.
func GetNodeAtPath(doc *yaml.Node, dotPath string) (*yaml.Node, error) {
	node := mappingRoot(doc)
	if dotPath == "" {
		return node, nil
	}
	parts := strings.Split(dotPath, ".")

	for _, key := range parts {
		if node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("expected mapping at %q, got kind %d", key, node.Kind)
		}
		found := false
		for i := 0; i < len(node.Content)-1; i += 2 {
			if node.Content[i].Value == key {
				node = node.Content[i+1]
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("key %q not found in path %q", key, dotPath)
		}
	}
	return node, nil
}

// GetMappingKeys returns all keys of a mapping node at the given dot-path.
func GetMappingKeys(doc *yaml.Node, dotPath string) ([]string, error) {
	node, err := GetNodeAtPath(doc, dotPath)
	if err != nil {
		return nil, err
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("node at %q is not a mapping", dotPath)
	}
	var keys []string
	for i := 0; i < len(node.Content)-1; i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys, nil
}

func mappingHasKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(
		node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func ensureMappingChild(node *yaml.Node, key string) (*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping while creating %q", key)
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value != key {
			continue
		}
		child := node.Content[i+1]
		if child.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("node %q is not a mapping", key)
		}
		return child, nil
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(
		node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		child,
	)
	return child, nil
}

func ensureMappingAtPath(doc *yaml.Node, dotPath string) (*yaml.Node, error) {
	node := mappingRoot(doc)
	if node == nil {
		return nil, fmt.Errorf("empty yaml document")
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("root is not a mapping")
	}
	if dotPath == "" {
		return node, nil
	}
	for _, key := range strings.Split(dotPath, ".") {
		child, err := ensureMappingChild(node, key)
		if err != nil {
			return nil, err
		}
		node = child
	}
	return node, nil
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	clone := *node
	if len(node.Content) > 0 {
		clone.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			clone.Content[i] = cloneYAMLNode(child)
		}
	}
	return &clone
}

func avatarModelConfigDir(doc *yaml.Node, configPath string) (string, bool, error) {
	node, err := GetNodeAtPath(doc, "inference.avatar.model_config_dir")
	if err != nil {
		return "", false, nil
	}
	if node.Kind != yaml.ScalarNode {
		return "", false, fmt.Errorf("inference.avatar.model_config_dir must be a scalar")
	}
	dir := strings.TrimSpace(NodeScalarValue(node, true))
	if dir == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(configPath), dir)
	}
	return dir, true, nil
}

func avatarModelConfigFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("avatar model config dir not found: %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("avatar model config dir is not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read avatar model config dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

func modelConfigFiles(dir, category string, requireDir bool) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) && !requireDir {
			return nil, nil
		}
		return nil, fmt.Errorf("%s model config dir not found: %s: %w", category, dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s model config dir is not a directory: %s", category, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s model config dir %s: %w", category, dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

func singleAvatarModelNode(path string) (string, *yaml.Node, error) {
	doc, err := ReadYAMLNode(path)
	if err != nil {
		return "", nil, err
	}
	root := mappingRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("avatar model config root must be a mapping: %s", path)
	}
	if len(root.Content) != 2 {
		return "", nil, fmt.Errorf("avatar model config file must contain exactly one top-level model: %s", path)
	}
	nameNode := root.Content[0]
	modelNode := root.Content[1]
	modelName := strings.TrimSpace(nameNode.Value)
	if modelName == "" {
		return "", nil, fmt.Errorf("avatar model config name must be non-empty: %s", path)
	}
	if modelNode.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("avatar model config value must be a mapping: %s", path)
	}
	return modelName, modelNode, nil
}

func singleModelNode(path, category string) (string, *yaml.Node, error) {
	doc, err := ReadYAMLNode(path)
	if err != nil {
		return "", nil, err
	}
	root := mappingRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("%s model config root must be a mapping: %s", category, path)
	}
	if len(root.Content) != 2 {
		return "", nil, fmt.Errorf("%s model config file must contain exactly one top-level model: %s", category, path)
	}
	nameNode := root.Content[0]
	modelNode := root.Content[1]
	modelName := strings.TrimSpace(nameNode.Value)
	if modelName == "" {
		return "", nil, fmt.Errorf("%s model config name must be non-empty: %s", category, path)
	}
	if modelNode.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("%s model config value must be a mapping: %s", category, path)
	}
	return modelName, modelNode, nil
}

// MergeAvatarModelConfigDir appends external avatar model mappings to
// inference.avatar. Inline model configs in the main file take precedence.
func MergeAvatarModelConfigDir(doc *yaml.Node, configPath string) error {
	avatar, err := GetNodeAtPath(doc, "inference.avatar")
	if err != nil || avatar.Kind != yaml.MappingNode {
		return nil
	}
	dir, ok, err := avatarModelConfigDir(doc, configPath)
	if err != nil || !ok {
		return err
	}
	files, err := avatarModelConfigFiles(dir)
	if err != nil {
		return err
	}
	externalModels := map[string]string{}
	for _, file := range files {
		modelName, modelNode, err := singleAvatarModelNode(file)
		if err != nil {
			return err
		}
		if previous, exists := externalModels[modelName]; exists {
			return fmt.Errorf("duplicate avatar model config for %q: %s and %s", modelName, previous, file)
		}
		externalModels[modelName] = file
		if mappingHasKey(avatar, modelName) {
			continue
		}
		avatar.Content = append(
			avatar.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: modelName},
			cloneYAMLNode(modelNode),
		)
	}
	return nil
}

func repoRootForConfig(configPath string) string {
	configDir := filepath.Clean(filepath.Dir(configPath))
	if filepath.Base(configDir) == "config" && filepath.Base(filepath.Dir(configDir)) == "infra" {
		return filepath.Dir(filepath.Dir(configDir))
	}
	if filepath.Base(configDir) == "config" {
		return filepath.Dir(configDir)
	}
	return configDir
}

func conventionalModelConfigDirs(configPath, dirName string) []string {
	repoRoot := repoRootForConfig(configPath)
	candidates := []string{
		filepath.Join(repoRoot, "infra", "config", dirName),
		filepath.Join(repoRoot, "config", dirName),
		filepath.Join(filepath.Dir(configPath), dirName),
	}
	dirs := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			abs = filepath.Clean(candidate)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		dirs = append(dirs, candidate)
	}
	return dirs
}

// MergeConventionalModelConfigDirs appends conventional external model
// mappings to inference.<category>. Built-in configs load first, local configs
// override them, and inline configs in the main file take precedence.
func MergeConventionalModelConfigDirs(doc *yaml.Node, configPath string) error {
	if _, err := ensureMappingAtPath(doc, "inference"); err != nil {
		return err
	}
	for _, category := range conventionalModelConfigCategories {
		section, err := ensureMappingAtPath(doc, "inference."+category.category)
		if err != nil {
			return err
		}

		inlineModels := map[string]bool{}
		for i := 0; i < len(section.Content)-1; i += 2 {
			if section.Content[i+1].Kind == yaml.MappingNode {
				inlineModels[section.Content[i].Value] = true
			}
		}

		for _, dir := range conventionalModelConfigDirs(configPath, category.dirName) {
			files, err := modelConfigFiles(dir, category.category, false)
			if err != nil {
				return err
			}
			seenInDir := map[string]string{}
			for _, file := range files {
				modelName, modelNode, err := singleModelNode(file, category.category)
				if err != nil {
					return err
				}
				if previous, exists := seenInDir[modelName]; exists {
					return fmt.Errorf("duplicate %s model config for %q: %s and %s", category.category, modelName, previous, file)
				}
				seenInDir[modelName] = file
				if inlineModels[modelName] {
					continue
				}
				setMappingValue(section, modelName, cloneYAMLNode(modelNode))
			}
		}
	}
	return nil
}

// AvatarModelConfigSource returns the file that owns writable params for a
// model. Inline model config in the main file wins over external configs.
func AvatarModelConfigSource(configPath, modelName string) (string, bool, error) {
	if strings.TrimSpace(modelName) == "" {
		return "", false, fmt.Errorf("avatar model name is empty")
	}
	doc, err := ReadYAMLNode(configPath)
	if err != nil {
		return "", false, err
	}
	if _, err := GetNodeAtPath(doc, "inference.avatar."+modelName); err == nil {
		return configPath, false, nil
	}
	dir, ok, err := avatarModelConfigDir(doc, configPath)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, fmt.Errorf("avatar model config not found for %q", modelName)
	}
	files, err := avatarModelConfigFiles(dir)
	if err != nil {
		return "", false, err
	}
	externalModels := map[string]string{}
	for _, file := range files {
		name, _, err := singleAvatarModelNode(file)
		if err != nil {
			return "", false, err
		}
		if previous, exists := externalModels[name]; exists {
			return "", false, fmt.Errorf("duplicate avatar model config for %q: %s and %s", name, previous, file)
		}
		externalModels[name] = file
	}
	if path, ok := externalModels[modelName]; ok {
		return path, true, nil
	}
	return "", false, fmt.Errorf("avatar model config not found for %q", modelName)
}

// AvatarModelExternalDotPath maps inference.avatar.<model>.* to <model>.*
// for writes into a single-model external avatar config file.
func AvatarModelExternalDotPath(modelName, fullPath string) (string, error) {
	prefix := "inference.avatar." + modelName + "."
	if !strings.HasPrefix(fullPath, prefix) {
		return "", fmt.Errorf("parameter %q is not in scope for model %q", fullPath, modelName)
	}
	return modelName + "." + strings.TrimPrefix(fullPath, prefix), nil
}

// SetNodeAtPath sets a scalar value at the given dot-path.
// It auto-detects numeric types so the YAML tag is correct.
func SetNodeAtPath(doc *yaml.Node, dotPath string, value string) error {
	node, err := GetNodeAtPath(doc, dotPath)
	if err != nil {
		return err
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("node at %q is not a scalar (kind %d)", dotPath, node.Kind)
	}

	node.Value = value
	node.Tag = inferYAMLTag(value)
	node.Style = 0 // reset style so yaml.v3 picks the natural representation
	return nil
}

// WriteYAMLNode atomically writes a yaml.Node tree back to disk.
func WriteYAMLNode(path string, doc *yaml.Node) error {
	yamlMu.Lock()
	defer yamlMu.Unlock()

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// NodeScalarValue returns the string value of a scalar node,
// optionally expanding ${ENV_VAR} references for display.
func NodeScalarValue(node *yaml.Node, expandEnv bool) string {
	if node.Kind != yaml.ScalarNode {
		return ""
	}
	v := node.Value
	if expandEnv && strings.Contains(v, "${") {
		v = os.ExpandEnv(v)
	}
	return v
}

// NodeValue returns the value as an appropriate Go type (string, int, float64, bool).
func NodeValue(node *yaml.Node, expandEnv bool) any {
	if node.Kind != yaml.ScalarNode {
		return node.Value
	}
	raw := node.Value
	display := raw
	if expandEnv && strings.Contains(raw, "${") {
		display = os.ExpandEnv(raw)
	}

	// Try int
	if i, err := strconv.ParseInt(display, 10, 64); err == nil {
		return i
	}
	// Try float
	if f, err := strconv.ParseFloat(display, 64); err == nil {
		return f
	}
	// Try bool
	if display == "true" {
		return true
	}
	if display == "false" {
		return false
	}
	return display
}

func inferYAMLTag(value string) string {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return "!!int"
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return "!!float"
	}
	if value == "true" || value == "false" {
		return "!!bool"
	}
	return "!!str"
}
