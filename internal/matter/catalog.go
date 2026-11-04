package matter

import (
	"strings"
	"unicode"
)

//go:generate go run ./gen

type memberKey struct{ cluster, id uint32 }

// Global attributes apply to every standard cluster and are not repeated in
// the cluster XML definitions used by the generator.
var globalAttributes = map[uint32]string{
	0xFFF8: "GeneratedCommandList",
	0xFFF9: "AcceptedCommandList",
	0xFFFA: "EventList",
	0xFFFB: "AttributeList",
	0xFFFC: "FeatureMap",
	0xFFFD: "ClusterRevision",
}

func deviceTypeName(id uint32) string { return standardDeviceTypes[id] }

func clusterName(id uint32) string { return standardClusters[id] }

func attributeName(clusterID, id uint32) string {
	cluster := clusterName(clusterID)
	if cluster == "" {
		return ""
	}
	name := attributeMemberName(clusterID, id)
	if name == "" {
		return ""
	}
	return cluster + "." + name
}

func attributeMemberName(clusterID, id uint32) string {
	if name := standardAttributes[memberKey{clusterID, id}]; name != "" {
		return name
	}
	return globalAttributes[id]
}

func commandName(clusterID, id uint32) string {
	return standardCommands[memberKey{clusterID, id}]
}

func eventName(clusterID, id uint32) string {
	name := standardEvents[memberKey{clusterID, id}]
	if name == "" {
		return ""
	}
	var out strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			previous := runes[i-1]
			if unicode.IsLower(previous) || unicode.IsDigit(previous) ||
				(unicode.IsUpper(previous) && i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
				out.WriteByte('_')
			}
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}
