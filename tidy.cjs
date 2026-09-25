const fs=require('fs');const edit=(p,f)=>fs.writeFileSync(p,f(fs.readFileSync(p,'utf8').replace(/\r\n/g,'\n')));
edit('internal/deposit/service.go',s=>s.replace(/\t\/\/ Development\/testing provider\.[\s\S]*?\t\/\/ MTN_MOMO remains pending/, '\t// SANDBOX creation and completion commit in one repository transaction.\n\t// MTN_MOMO remains pending'));
edit('internal/config/config.go',s=>s.replace('\treturn cfg',` edge,err:=decimal.NewFromString(cfg.HouseEdge);if err!=nil || edge.IsNegative() || edge.GreaterThanOrEqual(decimal.NewFromInt(1)){panic("invalid HOUSE_EDGE")}
 return cfg`));
edit('internal/cache/redis.go',s=>s.replace('\n\t"fmt"','').replace(/\nfunc \(s \*Store\) String\(\) string \{[\s\S]*?\n}/,''));
