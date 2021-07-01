package utils

import (
	"encoding/json"
	"strconv"
	"strings"
)

func Parse(ref string) (namespace string, name string) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func GetMapString(key string, m map[string]interface{}) string {
	a, ok := m[key]
	if ok {
		return a.(string)
	}
	return ""
}

func GetMapBoolean(key string, m map[string]interface{}) bool {
	a, ok := m[key]
	if ok {
		return strings.EqualFold(a.(string), "true")
	}
	return false
}

func GetMapInt64(key string, m map[string]interface{}) int64 {
	a, ok := m[key]
	if ok {
		num, err := strconv.Atoi(a.(string))
		if err != nil {
			return 0
		}
		return int64(num)
	}
	return 0
}

func GetArgValueByKey(key, args string) string {
	l := strings.Split(args, " ")
	length := len(l)
	for i := 0; i < length; i++ {
		if strings.Contains(l[i], key) && i+1 < length {
			return l[i+1]
		}
	}
	return ""
}

func ConvertMapToObj(m map[string]interface{}, obj interface{}) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, obj); err != nil {
		return err
	}
	return nil
}
