package policy

import (
	"net"
	"strconv"
	"testing"
)

func TestEngineChecksBlockedRules(t *testing.T) {
	engine := New(
		[]int{25},
		[]string{"*.tracking.example", "malware.test"},
		[]string{"203.0.113.0/24", "10.0.0.5"},
	)

	if decision := engine.CheckPort(25); !decision.Blocked {
		t.Fatal("expected port 25 to be blocked")
	}

	if decision := engine.CheckDomain("pixel.tracking.example"); !decision.Blocked {
		t.Fatal("expected wildcard domain to be blocked")
	}

	if decision := engine.CheckDomain("malware.test"); !decision.Blocked {
		t.Fatal("expected exact domain to be blocked")
	}

	if decision := engine.CheckIP(net.ParseIP("203.0.113.9")); !decision.Blocked {
		t.Fatal("expected CIDR IP to be blocked")
	}

	if decision := engine.CheckIP(net.ParseIP("10.0.0.5")); !decision.Blocked {
		t.Fatal("expected exact IP to be blocked")
	}
}

func TestCheckDomainBlocksBlacklistVariants(t *testing.T) {
	engine := New(nil, []string{"*.tracking.example", "malware.test", "bad.example.com"}, nil)

	blocked := []string{
		"malware.test",           // 精确匹配
		"MALWARE.Test",           // 大小写不敏感
		"malware.test:443",       // 带端口
		"pixel.tracking.example", // 通配符子域
		"tracking.example",       // 通配符基域本身
		"a.b.tracking.example",   // 多级子域
		"bad.example.com.",
	}
	for _, host := range blocked {
		decision := engine.CheckDomain(host)
		if !decision.Blocked {
			t.Errorf("expected host %q to be blocked", host)
		}
		if decision.Reason != "domain blocked" {
			t.Errorf("host %q: unexpected reason %q", host, decision.Reason)
		}
		if decision.RuleID == "" {
			t.Errorf("host %q: expected non-empty rule id", host)
		}
	}
}

func TestCheckDomainAllowsNonBlacklistedHosts(t *testing.T) {
	engine := New(nil, []string{"*.tracking.example", "malware.test"}, nil)

	allowed := []string{
		"example.com",              // 无关域名
		"nottracking.example",      // 后缀相似但不在通配区域
		"tracking.example.evil.io", // 前缀包含基域但域名不同
		"good.test",                // 同 TLD 不同主机
		"",                         // 空主机名
	}
	for _, host := range allowed {
		if decision := engine.CheckDomain(host); decision.Blocked {
			t.Errorf("expected host %q to be allowed, got %+v", host, decision)
		}
	}
}

func TestCheckPortBlocksBlacklistedPorts(t *testing.T) {
	engine := New([]int{25, 445, 3389}, nil, nil)

	for _, port := range []int{25, 445, 3389} {
		decision := engine.CheckPort(port)
		if !decision.Blocked {
			t.Errorf("expected port %d to be blocked", port)
			continue
		}
		if decision.RuleID != "port:"+strconv.Itoa(port) {
			t.Errorf("port %d: unexpected rule id %q", port, decision.RuleID)
		}
		if decision.Reason != "port blocked" {
			t.Errorf("port %d: unexpected reason %q", port, decision.Reason)
		}
	}
}

func TestCheckPortAllowsCommonWebPorts(t *testing.T) {
	engine := New([]int{25, 445, 3389}, nil, nil)

	for _, port := range []int{80, 443, 8080, 0} {
		if decision := engine.CheckPort(port); decision.Blocked {
			t.Errorf("expected port %d to be allowed, got %+v", port, decision)
		}
	}
}

func TestCheckIPAllowsUnlistedIPs(t *testing.T) {
	engine := New(nil, nil, []string{"203.0.113.0/24", "10.0.0.5"})

	for _, ip := range []string{"198.51.100.7", "10.0.0.6", "192.0.2.1"} {
		if decision := engine.CheckIP(net.ParseIP(ip)); decision.Blocked {
			t.Errorf("expected ip %s to be allowed, got %+v", ip, decision)
		}
	}
}
