package containersvc

import (
	"testing"
)

func TestVolumeSource(t *testing.T) {
	uuid := "uuid"
	expect := "uuid_{{.Task.Slot}}"
	source := GenVolumeSourceForSwarm(uuid)
	if source != expect {
		t.Fatalf("expect source %s, get %s", expect, source)
	}

	volName := "uuid_mycas-0"
	volName1 := GenVolumeSourceName(uuid, "mycas-0")
	if volName1 != volName {
		t.Fatalf("GenVolumeSourceName expect %s, get %s", volName, volName1)
	}

	jvolName := "journal_uuid"
	jvolName1 := GetServiceJournalVolumeName(uuid)
	if jvolName1 != jvolName {
		t.Fatalf("GenServiceJournalVolumeName expect %s, get %s", jvolName, jvolName1)
	}
}
