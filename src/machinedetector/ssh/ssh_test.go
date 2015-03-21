package ssh

import (
  "testing"
)

func TestSsh(t *testing.T) {
  addr := "x3.tc"
  alive := Ssh(addr, 50)
  
  t.Log("[%v]:[%v]\n", addr, alive)
}

