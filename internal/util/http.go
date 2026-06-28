package util

func Is2xx(status int) bool {
	return status >= 200 && status < 300
}
