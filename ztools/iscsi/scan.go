package iscsi

import (
	"errors"
	"fmt"
	"io"
	"net"
)

func inner(sn *[4]byte, index int) {
	switch {
	case index < 0:
		return
	case sn[index] == 0xff:
		sn[index] = 0
		index--
		inner(sn, index)
	default:
		sn[index]++
	}
}

func increment(sn *[4]byte) {
	inner(sn, 3)
}

func Scan(conn net.Conn, config *ISCSIConfig) (AuthLog, error) {
	authlog := AuthLog{conn.RemoteAddr().String(), []Target{}, false}
	p := Parameters{[]TextParameter{}, 0}

	p.AddTextParameter("InitiatorName", "iqn.1993-08.org.debian:01:f0f8de6d331")
	p.AddTextParameter("InitiatorAlias", "musk")
	p.AddTextParameter("SessionType", "Discovery")
	p.AddTextParameter("HeaderDigest", "None")
	p.AddTextParameter("DataDigest", "None")
	p.AddTextParameter("DefaultTime2Wait", "2")
	p.AddTextParameter("DefaultTime2Retain", "0")
	p.AddTextParameter("IFMarker", "No")
	p.AddTextParameter("OFMarker", "No")
	p.AddTextParameter("ErrorRecoveryLevel", "0")
	p.AddTextParameter("MaxRecvDataSegmentLength", "32768")

	CmdSN := [4]byte{0, 0, 0, 0}

	r := NewLoginRequest(p, CmdSN)
	res, err := r.MarshalBinary()
	if err != nil {
		return authlog, err
	}

	conn.Write(res)

	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	if err != nil {
		authlog.HadError = true
		return authlog, err
	}

	response := NewLoginResponse()
	err = response.UnmarshalBinary(buf)
	if err != nil && err != io.EOF {
		fmt.Println(err)
		return authlog, err
	}

	// success is denoted by StatusClass == 0
	if response.Header.(*LoginResponseHeader).StatusClass != 0 {
		authlog.HadError = true
		return authlog, errors.New("iSCSI-login failed")
	}

	//	fmt.Printf("%+v\n", response.Header)

	increment(&CmdSN)

	p = Parameters{[]TextParameter{}, 0}
	p.AddTextParameter("SendTargets", "All")
	r2 := NewTextRequest(p, CmdSN, CmdSN)
	res, err = r2.MarshalBinary()
	if err != nil {
		return authlog, err
	}

	//	fmt.Printf("%x\n", res)

	conn.Write(res)

	buf = make([]byte, 1024)
	_, err = conn.Read(buf)
	if err != nil {
		authlog.HadError = true
		return authlog, err
	}
	response2 := NewTextResponse()
	err = response2.UnmarshalBinary(buf)
	if err != nil && err != io.EOF {
		return authlog, err
	}

	//	response2.Data.Print()
	//	fmt.Printf("%+v\n", response2.Header)

	targets := map[string]string{}
	for i, target := range response2.Data.Data {
		if i%2 == 1 {
			targets[response2.Data.Data[i-1].Value] = target.Value
		}
		// limiting the amount of connections to any one particular machine
		if i > config.MaxConnections {
			break
		}
	}

	for target, ip := range targets {
		//increment(&CmdSN)
		conn2, err := net.Dial("tcp", conn.RemoteAddr().String())
		if err != nil {
			return authlog, err
		}
		defer conn2.Close()

		p := Parameters{[]TextParameter{}, 0}
		p.AddTextParameter("InitiatorName", "iqn.1993-08.org.debian:01:f0f8de6d331")
		//p.AddTextParameter("InitiatorName", "iqn.1994-05.com.redhat:4fd2932635a0")
		p.AddTextParameter("InitiatorAlias", fmt.Sprintf("%0"+fmt.Sprint(len(config.LocalLogin)/4*4+4)+"s", config.LocalLogin))
		p.AddTextParameter("TargetName", target)
		p.AddTextParameter("SessionType", "Normal")
		p.AddTextParameter("HeaderDigest", "None")
		p.AddTextParameter("DataDigest", "None")
		p.AddTextParameter("DefaultTime2Wait", "2")
		p.AddTextParameter("DefaultTime2Retain", "0")
		p.AddTextParameter("IFMarker", "No")
		p.AddTextParameter("OFMarker", "No")
		p.AddTextParameter("ErrorRecoveryLevel", "0")
		p.AddTextParameter("InitialR2T", "No")
		p.AddTextParameter("ImmediateData", "Yes")
		p.AddTextParameter("MaxBurstLength", "16776192")
		p.AddTextParameter("FirstBurstLength", "262144")
		p.AddTextParameter("MaxOutstandingR2T", "1")
		p.AddTextParameter("MaxConnections", "1")
		p.AddTextParameter("DataPDUInOrder", "Yes")
		p.AddTextParameter("DataSequenceInOrder", "Yes")
		p.AddTextParameter("MaxRecvDataSegmentLength", "262144")
		r = NewLoginRequest(p, CmdSN)
		res, err = r.MarshalBinary()
		if err != nil {
			return authlog, err
		}

		_, err = conn2.Write(res)
		if err != nil {
			authlog.HadError = true
			return authlog, err

		}

		buf = make([]byte, 1024)
		_, err = conn2.Read(buf)
		if err != nil {
			fmt.Println(err)
			authlog.HadError = true
			return authlog, err
		}

		response = NewLoginResponse()
		err = response.UnmarshalBinary(buf)
		if err != nil && err != io.EOF {
			authlog.HadError = true
			return authlog, err
		}

		if response.Header.(*LoginResponseHeader).StatusClass == 0 && response.Header.(*LoginResponseHeader).StatusDetail == 0 {
			authlog.Targets = append(authlog.Targets, Target{target, ip, true, false})
		} else {
			authlog.Targets = append(authlog.Targets, Target{target, ip, false, false})
		}

	}

	return authlog, nil
}
