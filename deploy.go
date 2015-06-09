package main

import (
	"encoding/json"
	"fmt"
	"github.com/kardianos/osext"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ServerEndpoint   string
	ServerPort       string
	ServerMethod     string
	RepoDir          string
	RepoBranch       string
	RepoBuildScript  string
	RepoRunScript    string
	RepoSecret       string
	RepoBranchCheck  bool
	ScriptDir        string
	ScriptAlwaysWait bool
	ScriptRunAtStart bool
}

type GitRes struct {
	Ref string
}

func main() {

	execFolder, err := osext.ExecutableFolder()
	if err != nil {
		fmt.Println("GoDeploy: Failed to get the executable path")
	}
	configFile, err := ioutil.ReadFile(filepath.Clean(execFolder + "/" + "godeploy.conf"))
	if err != nil {
		fmt.Println("GoDeploy: Failed to find the godeploy.conf (trying again): ", err)
		configFile, err = ioutil.ReadFile("godeploy.conf")
		if err != nil {
			fmt.Println("GoDeploy: Failed to find the godeploy.conf: ", err)
			return
		}
	}

	var conf Config

	err = json.Unmarshal(configFile, &conf)
	if err != nil {
		fmt.Println("GoDeploy: Failed to parse godeploy.conf: ", err)
		return
	}

	if conf.ScriptDir == "" {
		conf.ScriptDir = conf.RepoDir
	}

	var updateCommand = "cd " + conf.RepoDir + " && git checkout " + conf.RepoBranch + " && git pull"
	var buildCommand = strings.Fields(filepath.Clean(conf.ScriptDir + "/" + conf.RepoBuildScript))
	var command = strings.Fields(filepath.Clean(conf.ScriptDir + "/" + conf.RepoRunScript))
	var errChan = make(chan error)

	var tempCommand []string
	if conf.ScriptRunAtStart {
		tempCommand = command
	} else {
		tempCommand = []string{""}
	}

	var cmd = exec.Command("sh", "-c", updateCommand)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println("GoDeploy: Failed to update the repository", err)
	}

	cmd = exec.Command(buildCommand[0], buildCommand[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println("GoDeploy: Failed to build the project", err)
	}

	cmd = exec.Command(tempCommand[0], tempCommand[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	go func() {
		errChan <- cmd.Run()
	}()

	http.HandleFunc("/"+conf.ServerEndpoint, func(res http.ResponseWriter, req *http.Request) {
		if strings.EqualFold(req.Method, conf.ServerMethod) {
			var git GitRes

			if conf.RepoBranchCheck {
				reqBody, err := ioutil.ReadAll(req.Body)
				if err != nil {
					fmt.Println("GoDeploy: Failed to read request body: ", err)
					fmt.Fprintf(res, "GoDeploy: Failed to verify branch")
					return
				}
				err = json.Unmarshal(reqBody, &git)
				if err != nil {
					fmt.Println("GoDeploy: Failed to parse request body: ", err)
					fmt.Fprintf(res, "GoDeploy: Failed to verify branch")
					return
				}
			}

			if !conf.RepoBranchCheck || git.Ref == "refs/heads/"+conf.RepoBranch {
				select {
				default:
					fmt.Println("GoDeploy: Process still running")
					if !conf.ScriptAlwaysWait {
						err = cmd.Process.Signal(os.Interrupt)
						if err != nil {
							fmt.Println(err)
						}
					}
					ps, err := cmd.Process.Wait()
					if err != nil {
						fmt.Println(err)
					}
					if ps != nil && ps.Exited() {
						fmt.Println("GoDeploy: Process exited")
					} else {
						fmt.Println("GoDeploy: Process almost finished or no process state")
					}
					if err = <-errChan; err != nil {
						fmt.Printf("GoDeploy: Process exited: %s\n", err)
					} else {
						fmt.Println("GoDeploy: Process exited without error")
					}
				case e := <-errChan:
					if e != nil {
						fmt.Printf("GoDeploy: Process exited: %s\n", e)
					} else {
						fmt.Println("GoDeploy: Process exited without error")
					}
				}

				cmd = exec.Command("sh", "-c", updateCommand)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					fmt.Println("GoDeploy: Failed to update the repository", err)
				}

				cmd = exec.Command(buildCommand[0], buildCommand[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					fmt.Println("GoDeploy: Failed to build the project", err)
				}

				cmd = exec.Command(command[0], command[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				go func() {
					errChan <- cmd.Run()
				}()
			}
			fmt.Fprintf(res, "GoDeploy: Done")
		} else {
			fmt.Println("GoDeploy: Received ", req.Method, " request on the deployment endpoint")
		}
	})

	s := &http.Server{
		Addr:           ":" + conf.ServerPort,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	fmt.Println("GoDeploy: Listening on port ", conf.ServerPort, " for deployment requests")
	fmt.Println("GoDeploy: ", s.ListenAndServe())
}
