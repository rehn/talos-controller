package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// EtcdMember is a merged view of an etcd member and its status.
type EtcdMember struct {
	ID        string   `json:"id"`
	Hostname  string   `json:"hostname"`
	NodeIP    string   `json:"nodeIp"`
	IsLearner bool     `json:"isLearner"`
	IsLeader  bool     `json:"isLeader"`
	DBSize    int64    `json:"dbSize"`
	DBInUse   int64    `json:"dbSizeInUse"`
	RaftIndex uint64   `json:"raftIndex"`
	Errors    []string `json:"errors"`
	Healthy   bool     `json:"healthy"`
	Error     string   `json:"error,omitempty"`
}

// EtcdInfo is the cluster-wide etcd state.
type EtcdInfo struct {
	Members []EtcdMember `json:"members"`
	Alarms  []string     `json:"alarms"`
	Leader  string       `json:"leader"`
	Error   string       `json:"error,omitempty"`
}

// Etcd collects etcd membership and status from the given control plane nodes.
func (c *Cluster) Etcd(ctx context.Context, controlPlanes []Node) EtcdInfo {
	info := EtcdInfo{}

	if len(controlPlanes) == 0 {
		info.Error = "no control plane nodes known"

		return info
	}

	var members []*machineapi.EtcdMember

	for _, cp := range controlPlanes {
		cctx, cancel := nodeCtx(ctx, cp.IP, 6*time.Second)
		resp, err := c.talos.EtcdMemberList(cctx, &machineapi.EtcdMemberListRequest{})

		cancel()

		if err != nil || len(resp.Messages) == 0 {
			if err != nil {
				info.Error = shortErr(err)
			}

			continue
		}

		members = resp.Messages[0].Members
		info.Error = ""

		cctx, cancel = nodeCtx(ctx, cp.IP, 6*time.Second)
		if alarms, err := c.talos.EtcdAlarmList(cctx); err == nil {
			for _, m := range alarms.Messages {
				for _, a := range m.MemberAlarms {
					info.Alarms = append(info.Alarms, fmt.Sprintf("member %x: %s", a.MemberId, a.Alarm.String()))
				}
			}
		}

		cancel()

		break
	}

	// Per-member status, queried on each control plane.
	statusByID := map[uint64]*machineapi.EtcdMemberStatus{}
	ipByID := map[uint64]string{}
	errByIP := map[string]string{}

	var mu sync.Mutex

	var wg sync.WaitGroup

	for _, cp := range controlPlanes {
		wg.Add(1)

		go func(ip string) {
			defer wg.Done()

			cctx, cancel := nodeCtx(ctx, ip, 6*time.Second)
			defer cancel()

			st, err := c.talos.EtcdStatus(cctx)

			mu.Lock()
			defer mu.Unlock()

			if err != nil || len(st.Messages) == 0 || st.Messages[0].MemberStatus == nil {
				if err != nil {
					errByIP[ip] = shortErr(err)
				}

				return
			}

			ms := st.Messages[0].MemberStatus
			statusByID[ms.MemberId] = ms
			ipByID[ms.MemberId] = ip
		}(cp.IP)
	}

	wg.Wait()

	hostToIP := map[string]string{}
	for _, cp := range controlPlanes {
		hostToIP[cp.Name] = cp.IP
	}

	for _, m := range members {
		em := EtcdMember{
			ID:        fmt.Sprintf("%x", m.Id),
			Hostname:  m.Hostname,
			IsLearner: m.IsLearner,
			NodeIP:    hostToIP[m.Hostname],
		}

		if ip, ok := ipByID[m.Id]; ok {
			em.NodeIP = ip
		}

		if st, ok := statusByID[m.Id]; ok {
			em.DBSize = st.DbSize
			em.DBInUse = st.DbSizeInUse
			em.RaftIndex = st.RaftIndex
			em.Errors = st.Errors
			em.IsLeader = st.Leader == m.Id
			em.Healthy = len(st.Errors) == 0

			if em.IsLeader {
				info.Leader = m.Hostname
			}
		} else if e, ok := errByIP[em.NodeIP]; ok {
			em.Error = e
		} else {
			em.Error = "no status reported"
		}

		info.Members = append(info.Members, em)
	}

	return info
}
