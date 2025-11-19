package utils

import "os/exec"

func Run(cmd ...string) error {
	command := exec.Command(cmd[0], cmd[1:]...)

	return command.Run()
}
