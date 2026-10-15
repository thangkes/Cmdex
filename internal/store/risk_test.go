package store

import "testing"

func TestDetectRisk(t *testing.T) {
	cases := []struct {
		line, platform string
		want           int
	}{
		// critical on computers
		{"rm -rf /var/www/old", Linux, RiskCritical},
		{"sudo rm -r ./build", Linux, RiskCritical},
		{"rm --recursive x", Linux, RiskCritical},
		{"cd /tmp && rm -fr cache", Linux, RiskCritical},
		{"Remove-Item -Path C:\\old -Recurse -Force", Windows, RiskCritical},
		{"rd /s /q C:\\temp", Windows, RiskCritical},
		{"del /s /q *.log", Windows, RiskCritical},
		{"format D: /q", Windows, RiskCritical},
		{"robocopy C:\\a D:\\b /MIR /R:2", Windows, RiskCritical},
		{"sudo mkfs.ext4 /dev/sdb1", Linux, RiskCritical},
		{"sudo dd if=disk.img of=/dev/sdb bs=4M", Linux, RiskCritical},
		{"echo x > /dev/sda", Linux, RiskCritical},
		{":(){ :|:& };:", Linux, RiskCritical},
		{"shutdown /r /t 0", Windows, RiskCritical},
		{"sudo systemctl reboot", Linux, RiskCritical},
		{"sudo reboot", Linux, RiskCritical},
		{"Restart-Computer -Force", Windows, RiskCritical},
		{"sudo ufw disable", Linux, RiskCritical},
		{"netsh advfirewall set allprofiles state off", Windows, RiskCritical},
		{"sudo iptables -F", Linux, RiskCritical},
		{"pfctl -d", PfSense, RiskCritical},
		{"pfSsh.php playback enableallowallwan", PfSense, RiskCritical},
		{"DROP DATABASE shop;", Linux, RiskCritical},
		{"docker system prune -a", Linux, RiskCritical},
		// warning on computers
		{"rm notes.txt", Linux, RiskWarning},
		{"sudo systemctl restart nginx", Linux, RiskWarning},
		{"Restart-Service -Name Spooler -Force", Windows, RiskWarning},
		{"taskkill /PID 1234 /F", Windows, RiskWarning},
		{"kill 1234", Linux, RiskWarning},
		{"chmod 600 key.pem", Linux, RiskWarning},
		{"sudo apt install -y nginx", Linux, RiskWarning},
		{"winget upgrade --all", Windows, RiskWarning},
		{"sudo ufw allow 22/tcp", Linux, RiskWarning},
		{"netsh winsock reset", Windows, RiskWarning},
		{"ipconfig /release && ipconfig /renew", Windows, RiskWarning},
		{"sudo mount /dev/sdb1 /mnt", Linux, RiskWarning},
		{"chkdsk C: /f", Windows, RiskWarning},
		{"pfctl -k 1.2.3.4", PfSense, RiskWarning},
		{"easyrule block wan 1.2.3.4", PfSense, RiskWarning},
		{"shutdown /a", Windows, RiskNormal}, // cancels a shutdown
		{"git push --force origin main", Linux, RiskWarning},
		// normal
		{"ls -lah /var/log", Linux, RiskNormal},
		{"ipconfig /all", Windows, RiskNormal},
		{"Get-Process | Sort-Object CPU -Descending", Windows, RiskNormal},
		{"netstat -ano | findstr :443", Windows, RiskNormal},
		{"ping -c 4 8.8.8.8", Linux, RiskNormal},
		{"Format-Table Name,Status", Windows, RiskNormal},
		{"grep -rn shutdown_hook src/", Linux, RiskNormal},
		{"pfctl -sr", PfSense, RiskNormal},
		{"taskkill /?", Windows, RiskWarning}, // errs on the side of warning
		// Cisco
		{"write erase", Cisco, RiskCritical},
		{"wr er", Cisco, RiskCritical},
		{"erase startup-config", Cisco, RiskCritical},
		{"reload", Cisco, RiskCritical},
		{"delete flash:config.text", Cisco, RiskCritical},
		{"no vlan 10", Cisco, RiskCritical},
		{"reload in 10", Cisco, RiskWarning},
		{"conf t", Cisco, RiskWarning},
		{"interface Gi1/0/1", Cisco, RiskWarning},
		{"switchport access vlan 10", Cisco, RiskWarning},
		{"shutdown", Cisco, RiskWarning}, // a port, not the device
		{"no shut", Cisco, RiskWarning},
		{"copy running-config startup-config", Cisco, RiskWarning},
		{"ip route 0.0.0.0 0.0.0.0 10.0.0.1", Cisco, RiskWarning},
		{"show running-config | include vlan", Cisco, RiskNormal},
		{"sh ip int br", Cisco, RiskNormal},
		{"ping 10.0.0.1", Cisco, RiskNormal},
	}
	for _, c := range cases {
		if got := DetectRisk(c.line, c.platform); got != c.want {
			t.Errorf("%s (%s): %d, want %d", c.line, c.platform, got, c.want)
		}
	}
	if RiskOf(RiskWarning, "ls", Linux) != RiskWarning || RiskOf(RiskNormal, "rm -rf x", Linux) != RiskCritical || RiskOf(7, "ls", Linux) != RiskCritical {
		t.Fatal("RiskOf")
	}
}

// Development tools (tool packs): debugging ports, shared folders, memory
// dumps, keys, files overwritten.
func TestDetectRiskDevTools(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"java -agentlib:jdwp=transport=dt_socket,server=y,suspend=y,address=*:5005 -jar app.jar", RiskCritical},
		{"java -Xrunjdwp:transport=dt_socket,server=y,address=0.0.0.0:8000 Main", RiskCritical},
		{"java -agentlib:jdwp=transport=dt_socket,server=y,address=127.0.0.1:5005 -jar app.jar", RiskWarning},
		{"python -m http.server", RiskWarning},
		{"python3 -m http.server 8080 --bind 127.0.0.1", RiskWarning},
		{"py -3 -m http.server", RiskWarning},
		{"jmap -dump:live,format=b,file=heap.bin 1234", RiskWarning},
		{"jcmd 1234 GC.heap_dump /tmp/h.hprof", RiskWarning},
		{"keytool -genkeypair -v -keystore my.keystore -alias me", RiskWarning},
		{"keytool -storepasswd -keystore my.keystore", RiskWarning},
		{"jarsigner app.jar mykey", RiskWarning},
		{"jar cf app.jar *", RiskWarning},
		{"jar -xvf app.war", RiskWarning},
		{"mvn clean package", RiskWarning},
		{"mvn install -DskipTests", RiskWarning},
		{"mvn archetype:generate", RiskWarning},
		{"gradle clean build", RiskWarning},
		{"./gradlew wrapper --gradle-version 8.5", RiskWarning},
		{"gradle init --type basic", RiskWarning},
		{"pip cache purge", RiskWarning},
		{"pip freeze > requirements.txt", RiskWarning},
		{"Get-Process > procs.txt", RiskWarning},
		// only reading
		{"jarsigner -verify app.jar", RiskNormal},
		{"keytool -list -keystore my.keystore", RiskNormal},
		{"jar tf app.jar", RiskNormal},
		{"jmap -histo 1234", RiskNormal},
		{"jstack 1234", RiskNormal},
		{"mvn compile", RiskNormal},
		{"gradle test --info", RiskNormal},
		{"python -m json.tool data.json", RiskNormal},
		{"pip list --outdated", RiskNormal},
		{"find / -name x 2>/dev/null", RiskNormal},
		{"make 2>&1 | tee build.log", RiskNormal},
		{"echo done >> build.log", RiskNormal},
		{"cmd > /dev/null", RiskNormal},
	}
	for _, c := range cases {
		if got := DetectRisk(c.line, Both); got != c.want {
			t.Errorf("%q: %d, want %d", c.line, got, c.want)
		}
	}
}
