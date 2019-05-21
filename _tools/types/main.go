// Generates a report about every driver wich UASTv2 types does it use,
// both in actual test fixtures and though the source code.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"sync"

	"github.com/bblfsh/sdk/driver/manifest/discovery"
	"github.com/bblfsh/sdk/uast"
)

const repoRootPath = "./drivers/"

var (
	pprof = flag.Bool("pprof", false, "start pprof profiler http endpoing")
)

func main() {
	flag.Parse()

	if *pprof {
		pprofAddr := "localhost:6060"
		fmt.Fprintf(os.Stderr, "running pprof on %s\n", pprofAddr)
		go func() {
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				log.Fatal("cannot start pprof: %v", err)
			}
		}()
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	drivers, err := listDrivers()
	if err != nil {
		return fmt.Errorf("failed to list drivers: %s", err)
	}

	err = maybeCloneOrPullAll(drivers)
	if err != nil {
		return fmt.Errorf("failed to pull driver repos: %s", err)
	}

	uastTypes := findAllUastTypes()
	for _, driver := range drivers {
		analyzeFixtures(driver)
		analyzeCode(driver, uastTypes)
	}

	formatMarkdownTable(drivers, uastTypes)
	return nil
}

type driverStats struct {
	url          string
	language     string
	path         string
	fixturesUast map[string]int
	codeUast     map[string]int
}

// listDrivers lists all available drivers.
func listDrivers() ([]driverStats, error) {
	fmt.Fprintf(os.Stderr, "discovering all available drivers\n")
	langs, err := discovery.OfficialDrivers(context.TODO(), &discovery.Options{
		NoStatic: true,
	})
	if err != nil {
		return nil, err
	}
	drivers := make([]driverStats, 0, len(langs))
	for _, l := range langs {
		drivers = append(drivers, driverStats{
			language:     l.Language,
			url:          l.RepositoryURL(),
			path:         l.RepositoryURL()[strings.LastIndex(l.RepositoryURL(), "/"):],
			fixturesUast: make(map[string]int),
			codeUast:     make(map[string]int),
		})
	}
	fmt.Fprintf(os.Stderr, "%d drivers found, %v\n", len(langs), drivers)
	return drivers, nil
}

// maybeCloneOrPullAll either clones repos to path in local FS or, if already preset,
// pulls the latest master for each of them.
func maybeCloneOrPullAll(drivers []driverStats) error {
	fmt.Fprintf(os.Stderr, "cloning %d drivers to %s\n", len(drivers), repoRootPath)
	err := os.MkdirAll(repoRootPath, os.ModePerm)
	if err != nil {
		return err
	}

	var (
		wg        sync.WaitGroup
		concurent = make(chan int, 3)
	)
	for i := range drivers {
		wg.Add(1)
		go maybeCloneOrPullAsync(&drivers[i], &wg, concurent)
	}
	wg.Wait()
	return nil
}

func maybeCloneOrPullAsync(d *driverStats, wg *sync.WaitGroup, concurent chan int) error {
	defer wg.Done()

	concurent <- 1
	defer func() {
		<-concurent
	}()

	return maybeCloneOrPull(d)
}

func maybeCloneOrPull(d *driverStats) error {
	repoPath := path.Join(repoRootPath, d.path)
	_, err := os.Stat(repoPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return err
	}

	if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "%s does not exist, cloning from %s\n", repoPath, d.url)
		cmd := exec.Command("git", "clone", d.url+".git")
		cmd.Dir = repoRootPath
		err = cmd.Run()
		if err != nil {
			return err
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "%q exists, will 'git pull' instead\n", repoPath)
	cmd := exec.Command("git", "pull", "origin", "master")
	cmd.Dir = repoPath
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

type uastType struct {
	name string
}

func (u *uastType) isUsedIn() {

}

// find all types that embed uast.GenNode
func findAllUastTypes() []uastType {
	var out []uastType // TODO: load package, iterate all structs and check
	types := []interface{}{
		uast.Identifier{},
		uast.String{},
		uast.Bool{},
		uast.QualifiedIdentifier{},
		uast.Comment{},
		uast.Group{},
		uast.FunctionGroup{},
		uast.Block{},
		uast.Alias{},
		uast.Import{},
		uast.RuntimeImport{},
		uast.RuntimeReImport{},
		uast.InlineImport{},
		uast.Argument{},
		uast.FunctionType{},
		uast.Function{},
	}
	for _, typee := range types {
		out = append(out, uastType{reflect.TypeOf(typee).String()})
	}
	fmt.Fprintf(os.Stderr, "%d uast:* types found\n", len(out))
	return out
}

// analyzeFixtures goes though all fixtures, assuming the driver is cloned.
// It updates given driverStats with results.
func analyzeFixtures(driver driverStats) {
	// TODO:
	// Walk(./fixutres/*.sem.uast)
	//   for every line
	//      if line contains('uast:')
	//        typee := uastName.match(line)
	//        driver.fixturesUast[typee] += 1
}

// analyzeCode checks if any of the types are used by
// this driver's package, though analyzing it's AST.
func analyzeCode(driver driverStats, uasts []uastType) {
	// TODO:
	// load package
	// for _, typee := range uasts {
	//   if typee.isUsedIn(package) {
	//     driver.codeUast[typee]++
	//   }
	// }
	driver.codeUast["Identifier"]++
}

func formatMarkdownTable(drivers []driverStats, uastTypes []uastType) {
	fmt.Print(header)
	defer fmt.Print(footer)

	formatMarkdownTableHeader(drivers)
	for _, typee := range uastTypes {
		fmt.Printf("|%25s|", typee.name)
		for _, dr := range drivers {
			fmt.Printf(" %d/%d |", dr.fixturesUast[typee.name], dr.codeUast[typee.name])
		}
		fmt.Println()
	}
}

func formatMarkdownTableHeader(drivers []driverStats) {
	fmt.Printf("|%25s|", "")
	for _, dr := range drivers {
		fmt.Printf("%5s|", dr.language)
	}
	fmt.Print("\n| :---------------------- |")
	for range drivers {
		fmt.Printf(" :-- |")
	}
	fmt.Println()
}

const header = `<!-- Code generated by 'make types' DO NOT EDIT. -->
# UAST Types

For every UAST type in every Driver 2 numbers are reported:
 - _fixtures usage_ number of times this type was used in driver _fixtures_ (*.sem.uast files)
 - _code usage_ number of times this type was usind in driver mapping DSL code (normalizer.go)

in the format _<fixtures usage>/<code usage>_.

`

const footer = `
**Don't see your favorite AST construct mapped? [Help us!](join-the-community.md)**
`
