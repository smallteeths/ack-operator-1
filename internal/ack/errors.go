package ack

import "strings"

const (
	WaitingErrorCode = "Throttling"                                  // "Throttling.User" and "Throttling.API"
	WaitingErrorMsg  = "Request was denied due to user flow control" // can not call api for now

	WaitingStatusCode = "InvalidStatus"                         // "InvalidStatus.Forbidden" and "InvalidStatus.Unsupported"
	WaitingStatusMsg  = "cannot operate cluster where state is" // state of resource not allow operation now
)

func isThrottlingError(err error) bool {
	if err != nil {
		errMsg := err.Error()
		return strings.Contains(errMsg, WaitingErrorCode) &&
			strings.Contains(errMsg, WaitingErrorMsg)
	}
	return false
}

func isUnexpectedStatusError(err error) bool {
	if err != nil {
		errMsg := err.Error()
		return strings.Contains(errMsg, WaitingStatusCode) &&
			strings.Contains(errMsg, WaitingStatusMsg)
	}
	return false
}
