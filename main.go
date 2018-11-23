package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/viper"
)

const defaultFailedCode = 1

// Check error checking
func check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// createDirectory create directory
func createDirectory(path string, mode os.FileMode) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err = os.Mkdir(path, mode)
		if err != nil {
			log.Fatalln(err)
		}
		log.Printf("Create directory: %s", path)
	}
}

// ReadConfig read json environment file from directory
func readConfig(filename string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigName(filename)
	v.AddConfigPath(".")
	v.AutomaticEnv()
	err := v.ReadInConfig()
	return v, err
}

// RunCommand exec command and print stdout,stderr and exitCode
func runCommand(name string, args ...string) (stdout string, stderr string, exitCode int) {
	log.Println("run command:", name, args)
	var outbuf, errbuf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &outbuf
	cmd.Stderr = &errbuf

	err := cmd.Run()
	stdout = outbuf.String()
	stderr = errbuf.String()

	if err != nil {
		// try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			ws := exitError.Sys().(syscall.WaitStatus)
			exitCode = ws.ExitStatus()
		} else {
			// This will happen (in OSX) if `name` is not available in $PATH,
			// in this situation, exit code could not be get, and stderr will be
			// empty string very likely, so we use the default fail code, and format err
			// to string and set to stderr
			log.Printf("Could not get exit code for failed program: %v, %v", name, args)
			exitCode = defaultFailedCode
			if stderr == "" {
				stderr = err.Error()
			}
		}
	} else {
		// success, exitCode should be 0 if go is ok
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		exitCode = ws.ExitStatus()
	}
	if exitCode != 0 {
		log.Fatalf("command result, stdout: %v, stderr: %v, exitCode: %v", stdout, stderr, exitCode)
	}
	log.Printf("command result, stdout: %v, stderr: %v, exitCode: %v", stdout, stderr, exitCode)
	return
}

// buildCmdProject build repository and equal it with $PROJECT_NAME variable passed from Gitlab
func buildCmdProject(cmd string, commit string) []string {
	commands := strings.Split(cmd, ",")

	runCommand("bash", "-c", "echo git fetch --prune origin")
	runCommand("bash", "-c", fmt.Sprintf("echo git checkout %s", commit))

	for _, command := range commands {
		runCommand("bash", "-c", "echo %s", command)
	}
	return commands
}

// buildCmdSubdir build other repositories
func buildCmdSubdir(cmd string) []string {
	commands := strings.Split(cmd, ",")

	for _, command := range commands {
		runCommand("bash", "-c", "echo %s", command)
	}
	return commands
}

// gitInitBranch initialize empty repository and checkout to commit
func gitInitBranch(branch string, repo string, commit string) {
	runCommand("bash", "-c", "echo git init")
	runCommand("bash", "-c", fmt.Sprintf("echo git remote add -t %s -f origin %s", branch, repo))
	runCommand("bash", "-c", fmt.Sprintf("echo git checkout %s", commit))
}

// getSubstring get substring after a string.
func getSubstring(value string, a string) string {
	pos := strings.LastIndex(value, a)
	if pos == -1 {
		return ""
	}
	adjustedPos := pos + len(a)
	if adjustedPos >= len(value) {
		return ""
	}
	return value[adjustedPos:]
}

// compareHashBranch compare hash in passed branch name with host folder
func compareHashBranch(val string, array []string) (exists bool, index int, hostCommit string, branchCommit string, matchDir string) {
	exists = false
	index = -1

	for i, v := range array {
		// Get substring after a string in host folders
		hostCommit = getSubstring(v, "ontest-")
		log.Printf("Server directory: [%s] and substring: %s\n", v, hostCommit)
		matchDir = v
		// Get substring after a string in passed branch name
		branchCommit = getSubstring(val, "ontest-")
		log.Printf("Passed branch name: [%s] and substring: %s\n", val, branchCommit)
		if hostCommit == branchCommit {
			index = i
			exists = true
			log.Printf("Find match! Server directory: [%s] and passed branch name: [%s]\n substrings [%s == %s] equals ", v, val, hostCommit, branchCommit)
			return
		}
	}
	return
}

func readDir(conf string) []string {
	var dir []string

	lst, err := ioutil.ReadDir(conf)
	if err != nil {
		panic(err)
	}

	for _, val := range lst {
		if val.IsDir() {
			log.Printf("[%s]\n", val.Name())
			dir = append(dir, val.Name())
		}
	}
	return dir
}

func buildNoHashBranch(projectName string, refSlug string, repoURL string, commitSHA string, conf *viper.Viper) error {
	branchPath := filepath.Join(conf.GetString("hostdir"), refSlug)
	// Directory with passed projectName variable from Gitlab
	subdirProject := filepath.Join(branchPath, projectName)
	// Get all subdirectories
	subdirs := conf.GetStringMapString("subdirs")
	// Get passed projectName directory action
	action := conf.GetString(fmt.Sprintf("subdirs.%s.action", projectName))
	// Create branch directory
	createDirectory(branchPath, 0750)
	createDirectory(filepath.Join(branchPath, projectName), 0750)

	for dir := range subdirs {
		if dir == projectName {
			log.Printf("Chdir to %s", subdirProject)
			err := os.Chdir(subdirProject)
			if err != nil {
				return err
			}
			gitInitBranch(refSlug, repoURL, commitSHA)
			buildCmdProject(action, commitSHA)
		} else {
			// Get other subdirs from config
			subdirOther := filepath.Join(branchPath, dir)
			// Get action from config for other subdirs
			actionOther := conf.GetString(fmt.Sprintf("subdirs.%s.action", dir))
			// Get other repositories from config for other subdirs
			repo := conf.GetString(fmt.Sprintf("subdirs.%s.repo", dir))
			log.Printf("Chdir to %s", branchPath)
			err := os.Chdir(branchPath)
			if err != nil {
				return err
			}
			runCommand("bash", "-c", fmt.Sprintf("echo git clone %s", repo))
			log.Printf("Chdir to %s", subdirOther)
			err = os.Chdir(subdirOther)
			if err != nil {
				return err
			}
			buildCmdSubdir(actionOther)
		}
	}
	return nil
}

func buildHashBranch(projectName string, refSlug string, matchDir string, repoURL string, commitSHA string, exists bool, conf *viper.Viper) error {
	// Passesd branchname directory
	branchPath := filepath.Join(conf.GetString("hostdir"), refSlug)
	// Matched branchname directory
	matchPath := filepath.Join(conf.GetString("hostdir"), matchDir)
	// Directory with passed projectName variable from Gitlab
	subdirProject := filepath.Join(branchPath, projectName)
	// Directory with passed projectName variable from Gitlab and matched branchPath
	matchSubdirProject := filepath.Join(matchPath, projectName)
	// Get all subdirectories
	subdirs := conf.GetStringMapString("subdirs")
	// Get passed projectName directory action
	action := conf.GetString(fmt.Sprintf("subdirs.%s.action", projectName))

	// Check directory, if hash commits equal create ProjectName directory and other subdirectories, build files
	// If hash commits not equal build files only for ProjectName
	if exists != true {
		// Create directory only if there not directory with the same hash commit
		createDirectory(branchPath, 0750)
		createDirectory(filepath.Join(branchPath, projectName), 0750)
		//if !directoryExists(branchPath) {
		for dir := range subdirs {
			if dir == projectName {
				log.Printf("Chdir to %s", subdirProject)
				err := os.Chdir(subdirProject)
				if err != nil {
					return err
				}
				gitInitBranch(refSlug, repoURL, commitSHA)
				buildCmdProject(action, commitSHA)
			} else {
				// Get other subdirs from config
				subdirOther := filepath.Join(branchPath, dir)
				// Get action from config for other subdirs
				actionOther := conf.GetString(fmt.Sprintf("subdirs.%s.action", dir))
				// Get other repositories from config for other subdirs
				repo := conf.GetString(fmt.Sprintf("subdirs.%s.repo", dir))
				log.Printf("Chdir to %s", branchPath)
				err := os.Chdir(branchPath)
				if err != nil {
					return err
				}
				runCommand("bash", "-c", fmt.Sprintf("echo git clone %s", repo))
				log.Printf("Chdir to %s", subdirOther)
				err = os.Chdir(subdirOther)
				if err != nil {
					return err
				}
				buildCmdSubdir(actionOther)
			}
		}
		// If hash commits equal, build files only in ProjectName directory
	} else {
		for dir := range subdirs {
			if dir == projectName {
				log.Printf("Chdir to %s", matchSubdirProject)
				err := os.Chdir(matchSubdirProject)
				if err != nil {
					return err
				}
				buildCmdProject(action, commitSHA)
			}
		}
	}
	return nil
}

func main() {

	// Set the command line arguments
	var (
		refSlug = flag.String("refslug", "", "$CI_COMMIT_REF_NAME lowercased, shortened to 63 bytes, "+
			"and with everything except 0-9 and a-z replaced with -. No leading / trailing -. "+
			"Use in URLs, host names and domain names.")
		ciRepositoryURL = flag.String("repourl", "", "The URL to clone the Git repository")
		commitSHA       = flag.String("commitsha", "", "The commit revision for which project is built.")
		projectName     = flag.String("projectname", "", "The project name that is currently being built (actually it is "+
			"project folder name")
	)

	flag.Parse()

	// Load json configuration
	conf, err := readConfig("env")
	check(err)

	// Read directories in path from config file, return list of directories
	dirs := readDir(conf.GetString("hostdir")) // Compare commits, compare 8 digit commit hash of branch name with 8 digit commit hash of folder name,
	// used in conditions below
	//_, _, hostCommitSHA, branchCommitSHA, matchDir := compareCommits(*refSlug, dirs)
	exists, _, hostCommitSHA, branchCommitSHA, matchDir := compareHashBranch(*refSlug, dirs)

	switch branchCommitSHA {
	case "":
		switch *projectName {
		case "directoryA":
			err = buildNoHashBranch(*projectName, *refSlug, *ciRepositoryURL, *commitSHA, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryB":
			err = buildNoHashBranch(*projectName, *refSlug, *ciRepositoryURL, *commitSHA, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryC":
			err = buildNoHashBranch(*projectName, *refSlug, *ciRepositoryURL, *commitSHA, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryD":
			err = buildNoHashBranch(*projectName, *refSlug, *ciRepositoryURL, *commitSHA, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}

		// If 8 digit hash commit of server folder name matched with 8 digit hash commit of branch name (example:
		// branch name: 1-branch-ontest-de1234de
		// server folder name: 1-branch-ontest-de1234de
		// de1234de == de1234de 8 digit commit hash matched!)
	case hostCommitSHA:
		switch *projectName {
		case "directoryA":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryB":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryC":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryD":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}
		// If passed branchName(*refSlug) with hash commit not exists
		// (example branch name: 1-branch-ontest-de1234de)
	default:
		switch *projectName {
		case "directoryA":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryB":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryC":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		case "directoryD":
			err = buildHashBranch(*projectName, *refSlug, matchDir, *ciRepositoryURL, *commitSHA, exists, conf)
			if err != nil {
				log.Fatalf("Error: %s", err)
			}
		}
	}
}
