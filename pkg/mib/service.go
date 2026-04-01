package mib

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"
)

// Node represents a node in the MIB tree.
type Node struct {
	Name        string           `json:"name"`
	Oid         string           `json:"oid"`
	Description string           `json:"description"`
	Children    []*Node          `json:"children"`
	MibType     string           `json:"mibType"`
	Syntax      string           `json:"syntax"`
	Access      string           `json:"access"`
	EnumValues  map[string]int64 `json:"enumValues,omitempty"`
}

// Service handles MIB loading and parsing.
type Service struct {
	path string
}

// NewService creates a new MIB service that operates on the given path.
func NewService(mibPath string) *Service {
	return &Service{path: mibPath}
}

// LoadAll loads all MIBs from the service's path and returns the full tree.
func (s *Service) LoadAll() ([]*Node, error) {
	log.Println("Loading all MIBs from:", s.path)

	files, err := os.ReadDir(s.path)
	if err != nil {
		return nil, fmt.Errorf("could not read MIB directory %s: %v", s.path, err)
	}

	loadedModuleNames := []string{}
	for _, file := range files {
		fileName := file.Name()
		if !file.IsDir() && (strings.HasSuffix(strings.ToLower(fileName), ".mib") || strings.HasSuffix(strings.ToLower(fileName), ".txt")) {
			moduleName, err := gosmi.LoadModule(fileName)
			if err != nil {
				log.Printf("Warning: could not load MIB module '%s': %v", fileName, err)
			} else {
				log.Printf("Successfully loaded MIB module '%s' from file '%s'", moduleName, fileName)
				loadedModuleNames = append(loadedModuleNames, moduleName)
			}
		}
	}

	if len(loadedModuleNames) == 0 {
		log.Println("No MIB modules were loaded. The tree will be empty.")
	}

	return s.buildTree(loadedModuleNames)
}

// LoadSpecific loads only the specified MIB files from the service's path.
func (s *Service) LoadSpecific(fileNames []string) ([]*Node, error) {
	log.Printf("Loading %d specific MIBs from: %s", len(fileNames), s.path)

	if len(fileNames) == 0 {
		log.Println("No MIB files specified. The tree will be empty.")
		return []*Node{}, nil
	}

	loadedModuleNames := []string{}
	for _, fileName := range fileNames {
		moduleName, err := gosmi.LoadModule(fileName)
		if err != nil {
			log.Printf("Warning: could not load MIB module '%s': %v", fileName, err)
		} else {
			log.Printf("Successfully loaded MIB module '%s' from file '%s'", moduleName, fileName)
			loadedModuleNames = append(loadedModuleNames, moduleName)
		}
	}

	if len(loadedModuleNames) == 0 {
		log.Println("No MIB modules were loaded. The tree will be empty.")
	}

	return s.buildTree(loadedModuleNames)
}

func (s *Service) buildTree(loadedModuleNames []string) ([]*Node, error) {
	nodeMap := make(map[string]*Node)
	isChild := make(map[string]bool)

	for _, moduleName := range loadedModuleNames {
		module, err := gosmi.GetModule(moduleName)
		if err != nil {
			log.Printf("Warning: could not retrieve module '%s' after loading: %v", moduleName, err)
			continue
		}
		for _, node := range module.GetNodes() {
			if node.Oid == nil {
				continue
			}
			oidStr := node.Oid.String()
			if oidStr == "" {
				continue
			}
			if _, exists := nodeMap[oidStr]; !exists {
				newNode := &Node{
					Name:        node.Name,
					Oid:         oidStr,
					Description: node.Description,
					Children:    []*Node{},
					MibType:     node.Kind.String(),
					Access:      node.Access.String(),
				}
				if node.Type != nil {
					newNode.Syntax = node.Type.Name
					if node.Type.Enum != nil && len(node.Type.Enum.Values) > 0 {
						newNode.EnumValues = make(map[string]int64)
						for _, val := range node.Type.Enum.Values {
							newNode.EnumValues[val.Name] = val.Value
						}
					}
				}
				nodeMap[oidStr] = newNode
			}
		}
	}

	log.Printf("Created %d unique nodes with OIDs.", len(nodeMap))

	for _, mibNode := range nodeMap {
		oidStr := mibNode.Oid
		lastDot := strings.LastIndex(oidStr, ".")
		if lastDot != -1 {
			parentOidStr := oidStr[:lastDot]
			if parentNode, ok := nodeMap[parentOidStr]; ok {
				parentNode.Children = append(parentNode.Children, mibNode)
				isChild[oidStr] = true
			}
		}
	}

	var rootNodes []*Node
	for oid, mibNode := range nodeMap {
		if !isChild[oid] {
			rootNodes = append(rootNodes, mibNode)
		}
	}
	log.Printf("Identified %d root nodes.", len(rootNodes))

	for _, root := range rootNodes {
		sortNodes(root)
	}
	sort.Slice(rootNodes, func(i, j int) bool {
		return isOidLess(rootNodes[i].Oid, rootNodes[j].Oid)
	})

	log.Println("MIB tree processing complete.")
	return rootNodes, nil
}

// MibLoadResult represents the load result for a single MIB file.
type MibLoadResult struct {
	FileName string `json:"fileName"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// MibLoadResponse wraps the tree and diagnostics for a load operation.
type MibLoadResponse struct {
	Tree        []*Node         `json:"tree"`
	Diagnostics []MibLoadResult `json:"diagnostics"`
}

// LoadWithDiagnostics loads MIBs and returns both the tree and per-file diagnostics.
func (s *Service) LoadWithDiagnostics(fileNames []string) MibLoadResponse {
	log.Printf("Loading %d MIBs with diagnostics from: %s", len(fileNames), s.path)

	var diagnostics []MibLoadResult
	var loadedModuleNames []string

	if len(fileNames) == 0 {
		// Load all MIBs from directory
		files, err := os.ReadDir(s.path)
		if err != nil {
			log.Printf("Could not read MIB directory: %v", err)
			return MibLoadResponse{Tree: []*Node{}, Diagnostics: diagnostics}
		}
		for _, file := range files {
			fileName := file.Name()
			if !file.IsDir() && (strings.HasSuffix(strings.ToLower(fileName), ".mib") || strings.HasSuffix(strings.ToLower(fileName), ".txt")) {
				fileNames = append(fileNames, fileName)
			}
		}
	}

	for _, fileName := range fileNames {
		moduleName, err := gosmi.LoadModule(fileName)
		if err != nil {
			log.Printf("Diagnostic: failed to load '%s': %v", fileName, err)
			diagnostics = append(diagnostics, MibLoadResult{
				FileName: fileName,
				Success:  false,
				Error:    err.Error(),
			})
		} else {
			log.Printf("Diagnostic: loaded '%s' as module '%s'", fileName, moduleName)
			diagnostics = append(diagnostics, MibLoadResult{
				FileName: fileName,
				Success:  true,
			})
			loadedModuleNames = append(loadedModuleNames, moduleName)
		}
	}

	tree, err := s.buildTree(loadedModuleNames)
	if err != nil {
		log.Printf("Error building tree: %v", err)
		return MibLoadResponse{Tree: []*Node{}, Diagnostics: diagnostics}
	}

	return MibLoadResponse{Tree: tree, Diagnostics: diagnostics}
}

// OidDetails contains the translated information for a given OID.
type OidDetails struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// OidInfo contains detailed MIB information for an OID, including enum values.
type OidInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Syntax      string           `json:"syntax,omitempty"`
	EnumValues  map[string]int64 `json:"enumValues,omitempty"`
}

// GetOidDetails takes a raw OID string and returns its translated details if found.
func (s *Service) Translate(oid string) OidDetails {
	smiOid, err := types.OidFromString(oid)
	if err != nil {
		return OidDetails{Name: oid, Description: "Invalid OID format"}
	}

	node, err := gosmi.GetNodeByOID(smiOid)
	if err != nil {
		return OidDetails{Name: oid, Description: "OID not found in loaded MIBs"}
	}
	return OidDetails{Name: node.Name, Description: node.Description}
}

// ResolveOid returns detailed MIB info for a single OID, including enum values.
func (s *Service) ResolveOid(oid string) OidInfo {
	smiOid, err := types.OidFromString(oid)
	if err != nil {
		return OidInfo{Name: oid}
	}
	node, err := gosmi.GetNodeByOID(smiOid)
	if err != nil {
		return OidInfo{Name: oid}
	}
	info := OidInfo{Name: node.Name, Description: node.Description}
	if node.Type != nil {
		info.Syntax = node.Type.Name
		if node.Type.Enum != nil && len(node.Type.Enum.Values) > 0 {
			info.EnumValues = make(map[string]int64)
			for _, val := range node.Type.Enum.Values {
				info.EnumValues[val.Name] = val.Value
			}
		}
	}
	return info
}

// ResolveOids returns detailed MIB info for a batch of OIDs.
func (s *Service) ResolveOids(oids []string) map[string]OidInfo {
	result := make(map[string]OidInfo, len(oids))
	for _, oid := range oids {
		result[oid] = s.ResolveOid(oid)
	}
	return result
}

func sortNodes(node *Node) {
	if len(node.Children) > 0 {
		sort.Slice(node.Children, func(i, j int) bool {
			return isOidLess(node.Children[i].Oid, node.Children[j].Oid)
		})
		for _, child := range node.Children {
			sortNodes(child)
		}
	}
}

// ListMibFiles returns a list of MIB file names in the specified directory.
func ListMibFiles(dirPath string) ([]string, error) {
	var mibFiles []string

	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return mibFiles, err
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return mibFiles, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			name := entry.Name()
			if name[0] != '.' && name != "README" && name != "LICENSE" {
				mibFiles = append(mibFiles, name)
			}
		}
	}

	return mibFiles, nil
}

func isOidLess(oid1, oid2 string) bool {
	parts1 := strings.Split(oid1, ".")
	parts2 := strings.Split(oid2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		num1, err1 := strconv.Atoi(parts1[i])
		num2, err2 := strconv.Atoi(parts2[i])

		if err1 != nil || err2 != nil {
			return parts1[i] < parts2[i]
		}
		if num1 != num2 {
			return num1 < num2
		}
	}
	return len(parts1) < len(parts2)
}
