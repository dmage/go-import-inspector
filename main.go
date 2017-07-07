package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

func IsStandardPackage(importPath string) bool {
	x := strings.SplitN(importPath, "/", 2)
	return !strings.Contains(x[0], ".")
}

type PackageCache struct {
	mu       sync.Mutex
	packages map[string]*build.Package
}

func NewPackageCache() *PackageCache {
	return &PackageCache{
		packages: make(map[string]*build.Package),
	}
}

func (c *PackageCache) Get(importPath string) (*build.Package, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pkg, ok := c.packages[importPath]
	return pkg, ok
}

func (c *PackageCache) Put(pkg *build.Package) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packages[pkg.ImportPath] = pkg
}

type DependencyManager struct {
	packages *PackageCache

	mu           sync.Mutex
	dependencies map[string]map[string]struct{}
}

func NewDependencyManager() *DependencyManager {
	return &DependencyManager{
		packages:     NewPackageCache(),
		dependencies: make(map[string]map[string]struct{}),
	}
}

func (m *DependencyManager) Import(path string, srcDir string) (*build.Package, error) {
	pkg, err := build.Import(path, srcDir, 0)
	if err != nil {
		return nil, err
	}

	cachedPkg, ok := m.packages.Get(pkg.ImportPath)
	if ok {
		return cachedPkg, nil
	}

	m.packages.Put(pkg)

	for _, im := range pkg.Imports {
		if im == "C" {
			continue
		}

		importedPkg, err := m.Import(im, pkg.Dir)
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		if m.dependencies[pkg.ImportPath] == nil {
			m.dependencies[pkg.ImportPath] = make(map[string]struct{})
		}
		m.dependencies[pkg.ImportPath][importedPkg.ImportPath] = struct{}{}
		m.mu.Unlock()
	}

	return pkg, nil
}

func (m *DependencyManager) Get(path string, srcDir string) (*build.Package, []string, error) {
	findPkg, err := build.Import(path, srcDir, build.FindOnly)
	if err != nil {
		return nil, nil, err
	}

	pkg, ok := m.packages.Get(findPkg.ImportPath)
	if !ok {
		pkg, err = m.Import(path, srcDir)
		if err != nil {
			return nil, nil, err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	depsMap := m.dependencies[pkg.ImportPath]
	var deps []string
	if len(depsMap) > 0 {
		deps = make([]string, 0, len(depsMap))
		for im := range depsMap {
			deps = append(deps, im)
		}
	}

	return pkg, deps, nil
}

func (m *DependencyManager) addDeps(deps map[string]struct{}, path string, keep func(string) bool) {
	if !keep(path) {
		return
	}
	if _, ok := deps[path]; ok {
		return
	}
	deps[path] = struct{}{}
	for im := range m.dependencies[path] {
		m.addDeps(deps, im, keep)
	}
}

func (m *DependencyManager) CoundDepsRecursive(path string, keep func(string) bool) int {
	deps := make(map[string]struct{})
	m.addDeps(deps, path, keep)
	return len(deps)
}

var excludeStandard = flag.Bool("exclude-standard", false, "exclude standard packages")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s <importPath>\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}

	filter := func(importPath string) bool {
		return true
	}
	if *excludeStandard {
		filter = func(importPath string) bool {
			return !IsStandardPackage(importPath)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	dm := NewDependencyManager()
	_, deps, err := dm.Get(flag.Args()[0], cwd)
	if err != nil {
		log.Fatal(err)
	}

	sort.Strings(deps)

	for _, im := range deps {
		if !filter(im) {
			continue
		}
		fmt.Printf("%6d %s\n", dm.CoundDepsRecursive(im, filter), im)
	}
}
