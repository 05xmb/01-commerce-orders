package sign

import "testing"

func TestSignAndVerify(t *testing.T) {
	parts := PaySignParts("N1", "T1", "O1", 10000, 1700000000)
	s := Sign("secret", parts...)
	if !Verify("secret", s, parts...) {
		t.Fatal("合法签名应通过")
	}
	if Verify("secret", s, PaySignParts("N1", "T1", "O1", 9999, 1700000000)...) {
		t.Fatal("篡改金额应失败")
	}
	if Verify("other-secret", s, parts...) {
		t.Fatal("错误密钥应失败")
	}
}

func TestRefundSignParts(t *testing.T) {
	parts := RefundSignParts("RN1", "CR1", "R1", 100, "success", 1700000000)
	s := Sign("k", parts...)
	if !Verify("k", s, parts...) {
		t.Fatal("退款签名应通过")
	}
	if Verify("k", s, RefundSignParts("RN1", "CR1", "R1", 100, "fail", 1700000000)...) {
		t.Fatal("篡改结果应失败")
	}
}

func TestHashPassword(t *testing.T) {
	if HashPassword("123456") != HashPassword("123456") {
		t.Fatal("同一口令散列应一致")
	}
	if HashPassword("123456") == HashPassword("654321") {
		t.Fatal("不同口令散列应不同")
	}
}
