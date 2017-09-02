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
	if !VolumeSourceHasTaskSlot(volName) {
		t.Fatalf("volume %s should have task slot", volName)
	}
	if VolumeSourceHasTaskSlot(uuid) {
		t.Fatalf("volume %s should not have task slot", uuid)
	}

	uuid1, slot, err := ParseVolumeSource(volName)
	if err != nil || slot != 0 || uuid1 != uuid {
		t.Fatalf("ParseVolumeSource error %s, get %s %d, expect %s %d", err, uuid1, slot, uuid, 0)
	}

	volName1 := GenVolumeSourceName(uuid1, slot)
	if volName1 != volName {
		t.Fatalf("GenVolumeSourceName expect %s, get %s", volName, volName1)
	}
}
