package main

import (
	"fmt"
	"net"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listenPipe listens on a new named pipe that only the current user may open. It fails when the pipe
// exists already.
func listenPipe(path string) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(path, &winio.PipeConfig{
		// P: no inherited entries, so the DACL has the current user alone.
		SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
		// As dockerd's pipe: the docker CLI closes its side of an attach with an empty message.
		MessageMode:      true,
		InputBufferSize:  65536,
		OutputBufferSize: 65536,
	})
}

// killChildrenWithMe makes Windows end the processes that this process starts when it ends, however
// it ends, as the connections to WSL depend on it. The job handle is left open on
// purpose: closing it is what ends them.
func killChildrenWithMe() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("set up job object: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		return fmt.Errorf("assign to job object: %w", err)
	}
	return nil
}
