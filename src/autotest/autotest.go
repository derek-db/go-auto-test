package main

import (
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path"
	"sort"
	"strings"

	"os/exec"

	"github.com/howeyc/fsnotify"
)

var gopaths = strings.Split(os.Getenv("GOPATH"), ":")

func clean(dir string) string {
	for _, root := range gopaths {
		srcdir := root + "/src/"
		if strings.HasPrefix(dir, srcdir) {
			return dir[len(srcdir):]
		}
	}
	return dir
}

// map[import dir] -> file-path
var dirtopath = make(map[string]string)

// map[dir] -> [list of imported dirs]
var dependencies = make(map[string][]string)

// map[dir] -> [list of dependent dirs]
var dependents = make(map[string][]string)

func insert(m map[string][]string, k, v string) bool {
	values, _ := m[k]
	for _, vn := range values {
		if vn == v {
			return false
		}
	}
	m[k] = append(values, v)
	return true
}

func process(dir string, fset *token.FileSet, fw *fsnotify.Watcher) error {
	fmt.Println("process:", dir)
	cleandir := clean(dir)
	dirtopath[cleandir] = dir

	srcdir, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("Unable to read %s: %v", dir, err)
	}
	defer srcdir.Close()

	if err = fw.Watch(dir); err != nil {
		return fmt.Errorf("fsnotify error on %q: %v", dir, err)
	}

	list, err := srcdir.Readdir(-1)
	if err != nil {
		fmt.Errorf("Unable to list %s: %v", dir, err)
	}

	for _, entry := range list {
		filename := path.Join(dir, entry.Name())
		if strings.HasSuffix(entry.Name(), ".go") {
			ast, err := parser.ParseFile(fset, filename, nil, parser.ImportsOnly)
			if err == nil {
				for _, imprt := range ast.Imports {
					var path string
					if n, err := fmt.Sscanf(imprt.Path.Value, "%q", &path); n != 1 {
						return fmt.Errorf("Unable to parse import %s: %v", imprt.Path.Value, err)
					}
					if insert(dependencies, cleandir, path) {
						// fmt.Printf("%s: import %q\n", filename, path)
					}
					insert(dependents, path, cleandir)
				}
			} else {
				//fmt.Printf("%s: error %s\n", filename, err)
			}
		} else if entry.IsDir() {
			err := process(filename, fset, fw)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func contains(list []string, m string) bool {
	for _, v := range list {
		if v == m {
			return true
		}
	}
	return false
}

func remove(list *[]string, m string) bool {
	l := *list
	for i, v := range l {
		if v == m {
			*list = append(l[:i], l[i+1:]...)
			return true
		}
	}
	return false
}

func pop(list *[]string) (string, bool) {
	l := *list
	if len(l) == 0 {
		return "", false
	}
	*list = l[1:]
	return l[0], true
}

func builder(targets chan string, implied chan string) {
	var done chan struct{}
	var todoTargets = []string{}
	var todoImplied = []string{}
	for {
		select {
		case t := <-targets:
			// fmt.Println("target:", t)
			remove(&todoImplied, t)
			if !contains(todoTargets, t) {
				todoTargets = append(todoTargets, t)
			}
		case t := <-implied:
			// fmt.Println("implied:", t)
			if !contains(todoTargets, t) && !contains(todoImplied, t) {
				todoImplied = append(todoImplied, t)
			}
		case <-done:
			// fmt.Println("<-done")
			done = nil
		}
		if done == nil {
			target, f := pop(&todoTargets)
			if !f {
				target, f = pop(&todoImplied)
			}
			if f {
				done = make(chan struct{})
				go bld(target, done)
			}
		}
		//fmt.Printf("todo:\n! %v\n? %v\n", todoTargets, todoImplied)
	}
}

func bld(target string, done chan<- struct{}) {
	defer close(done)

	fmt.Println("building", target)
	dirpath, found := dirtopath[target]
	if !found {
		fmt.Fprintln(os.Stderr, "target no known: ", target)
		return
	}

	cmd := exec.Command("go", "test", "-v")
	cmd.Dir = dirpath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fd, err := os.Open(dirpath)
	if err == nil {
		files, err := fd.Readdirnames(-1)
		if err == nil {
			for _, name := range files {
				if strings.HasSuffix(name, ".go") &&
					!(name[0] == '.' || name[0] == '#') &&
					!strings.HasPrefix(name, "flycheck_") {
					if match, _ := build.Default.MatchFile(dirpath, name); match {
						cmd.Args = append(cmd.Args, name)
					} else {
						fmt.Println("Ignoring file: ", name)
					}
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to list %q\n", dirpath)
		return
	}

	//fmt.Println("running: go ", strings.Join(cmd.Args, " "))
	err = cmd.Run()
	if err == nil {
		fmt.Printf("built %s: success!\n", target)
	} else {
		fmt.Printf("built %s: %v\n", target, err)
	}
}

func main() {
	if len(gopaths) == 0 {
		panic("GOPATH not set")
	}
	fw, _ := fsnotify.NewWatcher()
	for _, root := range gopaths {
		fset := token.NewFileSet()
		if err := process(path.Join(root, "src"), fset, fw); err != nil {
			panic(err)
		}
	}

	if false {
		fmt.Println("Dependencies:")
		keys := make([]string, 0, len(dependencies))
		for k, _ := range dependencies {
			keys = append(keys, k)
		}
		sort.Sort(sort.StringSlice(keys))
		for _, dir := range keys {
			deps := dependencies[dir]
			sort.Sort(sort.StringSlice(deps))
			fmt.Printf("%s <- %v\n", dir, deps)
		}
	}

	if true {
		fmt.Println("Depents:")
		keys := make([]string, 0, len(dependents))
		for k, _ := range dependents {
			keys = append(keys, k)
		}
		sort.Sort(sort.StringSlice(keys))
		for _, dir := range keys {
			deps := dependents[dir]
			sort.Sort(sort.StringSlice(deps))
			fmt.Printf("%s -> %v\n", dir, deps)
		}
	}

	targets := make(chan string)
	indirecttargets := make(chan string)
	go builder(targets, indirecttargets)

	for {
		select {
		case err := <-fw.Error:
			panic(err)
		case event := <-fw.Event:
			// fmt.Printf("event: %v\n", event)
			if strings.HasPrefix(path.Base(event.Name), "flycheck_") {
				continue
			}
			dirpath := path.Dir(event.Name)
			dir := clean(dirpath)
			process(dirpath, token.NewFileSet(), fw)
			targets <- dir
			deps := dependents[dir]
			sort.Sort(sort.StringSlice(deps))
			// fmt.Printf("%s -> %v\n", dir, deps)
			for _, it := range deps {
				indirecttargets <- it
			}
		}
	}
}
