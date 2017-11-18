package containersvc

import (
	"testing"
)

func TestVolumeSource(t *testing.T) {
	uuid := "uuid"
	expect := "uuid-{{.Task.Slot}}"
	source := GenVolumeSourceForSwarm(uuid)
	if source != expect {
		t.Fatalf("expect source %s, get %s", expect, source)
	}

	volName := "uuid-0"
	volName1 := GenVolumeSourceName(uuid, 0)
	if volName1 != volName {
		t.Fatalf("GenVolumeSourceName expect %s, get %s", volName, volName1)
	}
}
