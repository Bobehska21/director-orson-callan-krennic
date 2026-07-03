# Windows služba

`install.ps1` registruje Krennic jako Windows službu přes `sc.exe`:

```powershell
sc.exe create Krennic binPath= "`"%LOCALAPPDATA%\Krennic\krennic.exe`" run" start= auto
sc.exe start Krennic
```

Poznámky:
- Služba musí běžet **jako přihlášený uživatel**, aby měla přístup ke Credential
  Manageru/DPAPI (tam jsou tajemství) a k souborům vývojáře. Ve firemním nasazení
  nastav účet služby přes `sc.exe config Krennic obj= ".\<user>" password= "<pwd>"`
  nebo použij Task Scheduler s "Run only when user is logged on".
- Logy: přesměruj přes wrapper nebo použij Event Log; `krennic run` píše na stderr.
- Odinstalace: `uninstall.ps1` (`sc.exe stop/delete Krennic`).
