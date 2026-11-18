# taskledger: thiết kế bản 1

taskledger là một công cụ dòng lệnh để theo dõi nhiều task cùng lúc, dù các task đó thuộc những loại công việc khác nhau: việc ở công ty, việc học, dự án cá nhân. Lệnh là tlg. Người dùng là một người trên một máy. Họ gõ lệnh trực tiếp, hoặc để một agent AI (Claude Code, opencode, agy...) gõ thay.

Mỗi loại công việc có cách làm riêng: có loại đi qua nhiều giai đoạn với cửa duyệt, có loại chỉ là một cuốn sổ ghi chép. taskledger không cố gộp các cách làm đó thành một. Nó tách ra hai phần: một core chung lo những việc giống nhau ở mọi loại công việc, và các plugin, mỗi plugin lo một loại.

Tài liệu này viết cho người muốn hiểu taskledger hoạt động ra sao, hoặc muốn viết một plugin mới. Mỗi quyết định có kèm lý do, để người sửa code biết chỗ nào không được nới.

## Các khái niệm

| Khái niệm | Nghĩa |
|---|---|
| Core | Chương trình tlg. Giữ sổ đăng ký, vẽ board, ghi nhận phiên agent, chuyển lệnh sang plugin. Không biết gì về nghiệp vụ của task |
| Plugin | Một chương trình chạy riêng, thực hiện giao thức của core cho một loại công việc. Plugin quyết task gồm những file gì, lịch sử trông thế nào, task đang ở trạng thái nào |
| Sổ đăng ký | Cơ sở dữ liệu của core: danh sách task, quan hệ cha con, phiên agent |
| Chuỗi định vị | Chuỗi do plugin sinh ra khi tạo task, core lưu lại và truyền ngược cho plugin ở mọi lần gọi. Core không đọc nghĩa của nó |
| Trạng thái công việc | Công việc đang ở đâu, một trong 5 giá trị cố định. Plugin quyết |
| Trạng thái trong sổ | Người dùng có còn muốn thấy task trên board không. Core giữ |
| Phiên | Một cuộc hội thoại của một agent, định danh bằng tên agent và session id của agent đó |
| Board | Bảng tổng quan mọi task đang theo dõi, câu trả lời cho câu hỏi "nên làm gì tiếp" |

## Các thành phần nằm ở đâu?

| Thành phần | Vị trí mặc định | Định dạng |
|---|---|---|
| Core | Binary tlg, build từ repo này | Go |
| Plugin | Bất kỳ chương trình chạy được nào, khai trong cấu hình | Tuỳ plugin |
| Cấu hình | ~/.config/taskledger/config.toml | TOML |
| Sổ đăng ký | ~/.local/share/taskledger/registry.db | SQLite |

Plugin đứng ngoài repo core để không plugin nào được ưu ái: mọi plugin chỉ nói chuyện với core qua giao thức. Một plugin có thể viết bằng bất kỳ ngôn ngữ nào đọc được stdin và ghi được stdout.

Core viết bằng Go vì nó phải chặt ở những việc shell script làm kém: kiểm tra dữ liệu plugin trả về, xử lý lỗi khi gọi nhiều plugin song song, in bảng thẳng cột khi dữ liệu có chữ có dấu (printf của bash căn lề theo byte, nên cột lệch ở mọi dòng có dấu). Go cho một binary không phụ thuộc runtime. SQLite dùng thư viện viết thuần Go để giữ được điều đó. Độ rộng hiển thị của chuỗi đo theo ô màn hình, không theo byte hay số ký tự.

## Ngôn ngữ trong code và dữ liệu

- Code, comment, help, message lỗi: tiếng Anh.
- Dữ liệu (tên task, nội dung lịch sử, việc tiếp theo): tuỳ người dùng.
- Chuỗi thuộc giao thức hoặc do core sinh ra là tiếng Anh: 5 trạng thái công việc, tên nhóm trên board, cờ do core gắn.
- Chuỗi do plugin trả về (nhãn giai đoạn, việc tiếp theo, cờ của plugin) thì core in nguyên văn, bằng ngôn ngữ nào cũng được.
- Không làm i18n.

Một dòng board vì thế có thể trộn hai ngôn ngữ. Ranh giới: chữ in hoa và các cờ của core là của core, phần còn lại là của plugin.

## Task có những trạng thái nào?

Mỗi task có hai loại trạng thái, độc lập với nhau.

Trạng thái công việc, do plugin trả về:

| Giá trị | Nghĩa | Nhóm trên board |
|---|---|---|
| needs-me | Đang chờ người dùng quyết một việc | NEEDS ME, nhóm đầu |
| active | Đang làm | ACTIVE |
| waiting | Kẹt vì chờ người khác hoặc chờ một sự kiện | WAITING |
| paused | Người dùng chủ động dừng, sẽ làm lại | PAUSED |
| closed | Công việc xong | CLOSED, nhóm cuối |

Tập này cố định trong core, plugin không thêm giá trị mới. Lý do: task của mọi plugin nằm chung một board, và chỉ với một tập chung thì board mới gộp nhóm được và trả lời chung được câu "nên làm gì tiếp". Sắc thái riêng của plugin (ví dụ "đang theo dõi sau khi phát hành") đi vào nhãn giai đoạn và việc tiếp theo.

Trạng thái trong sổ, do core giữ:

| Giá trị | Hiện trên board | Hiện trong ls | Core gọi plugin lấy trạng thái | Phiên |
|---|---|---|---|---|
| tracked | Có | Có | Có | Giữ |
| archived | Không | Chỉ khi có cờ --all | Không | Giữ |

- Task archived không bị gọi status, nên board không chậm dần khi task cũ nhiều lên, và plugin không phải giữ khả năng đọc task cũ mãi mãi. Vì vậy sổ lưu bản sao tên task, cập nhật mỗi lần status trả về, để ls --all vẫn in được tên.
- Task closed vẫn nằm ở nhóm CLOSED cho tới khi người dùng archive. Core không tự archive sau một số ngày.
- Archive một task chưa closed vẫn được, core chỉ trả cảnh báo.
- forget xoá hẳn task khỏi sổ, kèm danh sách phiên. Chỉ dùng khi thêm nhầm. Lệnh này tách riêng khỏi archive cho khó gõ nhầm.

## Sổ đăng ký lưu những gì?

Một file SQLite. Chọn SQLite vì nhiều tiến trình có thể ghi cùng lúc (hai agent cùng gắn phiên đúng lúc người dùng thêm task), và SQLite tự khoá khi ghi. Sổ mở ở chế độ WAL, chờ khoá tối đa 5 giây. Sổ không đồng bộ giữa các máy và không theo dõi bằng git.

```sql
CREATE TABLE tasks (
  id          TEXT PRIMARY KEY,
  plugin      TEXT NOT NULL,
  locator     TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  parent_id   TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  registry    TEXT NOT NULL DEFAULT 'tracked' CHECK (registry IN ('tracked', 'archived')),
  created_at  TEXT NOT NULL
);

CREATE TABLE sessions (
  task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  agent       TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  cwd         TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  started_at  TEXT NOT NULL,
  last_seen   TEXT NOT NULL,
  PRIMARY KEY (task_id, agent, session_id)
);

CREATE TABLE hello_cache (
  plugin   TEXT PRIMARY KEY,
  command  TEXT NOT NULL,
  mtime    INTEGER NOT NULL,
  body     TEXT NOT NULL
);
```

Bảng hello_cache giữ câu trả lời hello của từng plugin, xem mục phiên bản giao thức.

Thời điểm lưu dạng RFC 3339 kèm múi giờ. Schema đánh số phiên bản bằng PRAGMA user_version, mỗi lần đổi schema là một bước migrate chạy tự động khi mở sổ. Bảng phụ thuộc và bảng nhãn thuộc bản 2, thêm bằng bước migrate.

Luật cho task id:

- Duy nhất trong toàn sổ, mọi plugin.
- Chỉ gồm chữ ASCII, số, gạch dưới, gạch ngang, dấu chấm, và bắt đầu bằng chữ hoặc số. Lý do: id xuất hiện trong lệnh shell, tên thư mục, tên file, và agent gõ nó rất nhiều lần.
- Không trùng tên một động từ của core, vì dạng lệnh tlg {task id} {động từ} sẽ không phân biệt được.
- Người dùng truyền id khi tạo task. Không truyền thì plugin đề xuất.

## Cha con hoạt động thế nào?

- Mỗi task có tối đa một cha, lưu ở cột parent_id. Số tầng không giới hạn. Core chặn vòng lặp khi đặt cha.
- Cha và con được thuộc hai plugin khác nhau. Ví dụ một mục tiêu quý thuộc plugin ghi chép cá nhân có con là các task thuộc plugin công việc.
- Một task cần nằm dưới hai mục tiêu thì dùng cha cho mục tiêu này, nhãn (bản 2) cho mục tiêu kia. Một cha duy nhất giữ cho board vẽ được thành cây.
- Core không truyền quan hệ cha con sang plugin, trừ một chỗ: khi tạo task con cùng plugin với cha, core truyền chuỗi định vị của cha vào lệnh new. Plugin chỉ nhận lời gọi này khi đã khai children là true trong hello, và vẫn được quyền từ chối.
- Nhiều phiên agent làm song song trên một task thì mỗi việc song song là một task con. Plugin tự quyết task con nghĩa là gì với nó, ví dụ một thư mục con nằm trong thư mục của cha.
- Id con mặc định là {id cha}.{tên ngắn}, với tên ngắn sinh từ tên task: bỏ dấu, chữ thường, nối bằng gạch ngang, tối đa 24 ký tự. Ví dụ task "Đọc chương 4" dưới cha RUST thành RUST.doc-chuong-4. Plugin đề xuất id khác thì core dùng id của plugin.
- Trạng thái công việc của cha do plugin của cha trả, như mọi task. Core không suy trạng thái cha từ các con, vì core không biết quan hệ cha con nghĩa là gì về nghiệp vụ: một mục tiêu có con đóng hết chưa chắc đã xong, còn một task vừa là cha vừa có quy trình riêng. Thay vào đó, khi mọi con đã closed mà cha chưa closed, core gắn cờ all children closed lên cha.
- Cha bị archive thì con tracked của nó hiện như một cây riêng, kèm cờ parent archived. Con archived không hiện dưới cha tracked.

## Board trông thế nào?

Board mặc định vẽ cây cha con. Cả một cây được xếp vào nhóm gấp nhất trong số các task của cây, theo thứ tự needs-me, active, waiting, paused, closed. Dòng nào có trạng thái riêng đúng bằng nhóm của cây thì có dấu * ở đầu, để người dùng nhìn ra dòng nào thật sự cần làm trong một cây dài.

Ví dụ với hai plugin: work (task có giai đoạn và cửa duyệt) và notes (sổ ghi chép cá nhân).

```
6 tasks tracked

NEEDS ME
  GOAL-Q4              notes  Mục tiêu quý 4         next               15/11 08:00  3d     --> rà tiến độ các task con
* ├─ WRK-101           work   Thêm API báo cáo       S4 review done     18/11 12:37  today  --> duyệt thiết kế
  └─ WRK-102           work   Sửa lỗi đăng nhập      S5 coding          16/11 10:08  2d     --> viết unit test
* WRK-103              work   Nâng cấp thư viện      S7 gate failed     18/11 14:36  today  --> quyết làm lại hay bỏ

ACTIVE
* RUST                 notes  Học Rust               start              18/11 17:56  today
  └─ RUST.doc-chuong-4 notes  Đọc chương 4           wait               18/11 17:56  today  --> sách: đang đặt mua

WAITING
* WRK-090              work   Cấu hình máy chủ mới   S9 waiting         10/11 11:40  8d     --> ops: deploy  [due 2026-11-12; overdue]
```

Cột theo thứ tự: dấu *, id (thụt theo tầng), plugin, tên (cắt theo độ rộng hiển thị), nhãn giai đoạn, thời điểm hoạt động cuối, bao lâu không động (today hoặc Nd), việc tiếp theo, rồi trong ngoặc vuông là hạn, các cờ và nhãn.

Trong ví dụ, cây GOAL-Q4 nằm ở NEEDS ME vì WRK-101 cần người dùng duyệt, dù bản thân GOAL-Q4 đang active. Cây RUST nằm ở ACTIVE và chỉ RUST có dấu *, vì con của nó đang chờ.

Sắp xếp:

- Các cây trong một nhóm: cây có task lâu không động nhất (trong số các task mang dấu *) đứng đầu.
- Các con dưới một cha: theo thứ tự trạng thái như trên, rồi theo bao lâu không động.

Cờ do core gắn:

| Cờ | Khi nào |
|---|---|
| all children closed | Mọi con đã closed, cha chưa closed |
| parent archived | Cha đã archive, task này vẫn tracked |
| overdue | Có hạn, hạn đã qua, task chưa closed |
| plugin not found | Cấu hình không còn plugin của task, hoặc không chạy được chương trình của plugin |
| plugin needs update | Plugin trả phiên bản giao thức core không nhận |
| plugin error: {lý do} | Plugin chết, quá thời gian, hoặc trả bản ghi sai |
| pull failed: {lý do} | board --pull gọi pull cho task và plugin báo lỗi |

Task không lấy được trạng thái vẫn hiện trên board, ở nhóm NEEDS ME, vì người dùng phải sửa nó. Lý do: một plugin hỏng không được làm task của nó biến mất mà không ai biết.

Tuỳ chọn của lệnh board:

- --pull: trước khi dựng board, gọi động từ pull cho các task tracked của mọi plugin có hỗ trợ, để plugin làm mới dữ liệu kéo từ nguồn ngoài (ticket, issue tracker). Một task kéo lỗi thì dòng đó mang cờ lỗi, board vẫn dựng.
- --json: in dữ liệu thô thay cho bảng.
- --flat (bản 2): chia nhóm theo trạng thái riêng của từng task, không vẽ cây, dòng con ghi thêm id cha.
- --tag (bản 2): lọc theo nhãn.

Core gọi status cho mọi task tracked, song song, tối đa 8 tiến trình plugin cùng lúc.

## Lệnh tlg gồm những gì?

Có hai dạng: lệnh chung, và lệnh nhắm vào một task.

```
tlg board [--pull] [--json]
tlg ls [--all] [--plugin <name>]
tlg new <plugin> [--id <id>] [--parent <id>] <title> [-- <plugin args>...]
tlg track <plugin> <id> [--locator <locator>]
tlg guide [<topic>]
tlg config edit
tlg plugin add <name> <command> [--timeout <dur>] [--env KEY=VALUE]... | plugin rm <name> | plugin ls
tlg doctor
tlg help

tlg <id>                      (giống tlg <id> show)
tlg <id> show
tlg <id> log <plugin args>...
tlg <id> guide [<topic>]
tlg <id> sessions
tlg <id> attach <agent> <session id> [--cwd <dir>] [--title <text>]
tlg <id> parent <parent id> | parent --none
tlg <id> archive | unarchive | forget
tlg <id> <bất kỳ động từ nào khác> <args>...
```

- Động từ không phải của core đứng sau task id thì được chuyển nguyên văn sang plugin của task. Mọi lệnh nghiệp vụ của plugin đều đi qua core, để phiên được gắn tự động, và để agent chỉ phải biết một chương trình. Plugin không được đặt tên lệnh trùng động từ của core.
- track thêm vào sổ một task đã tồn tại ở plugin. Không truyền chuỗi định vị thì core gọi động từ locate của plugin.
- new mặc định đặt task ở tracked. Phần sau dấu -- được chuyển nguyên văn cho plugin, ví dụ link ticket.
- Không có lệnh xoá dữ liệu task. Xoá thư mục hay file của task là việc khó quay lại, plugin nào muốn thì làm thành lệnh riêng qua đường chuyển tiếp.

Lệnh nào cũng không được chờ bàn phím, trừ config edit. Agent gọi lệnh chờ stdin thì phiên agent bị treo.

Định dạng đầu ra:

- board mặc định in bảng chữ, vì người dùng đọc nó trực tiếp trong terminal. Có --json thì in JSON.
- Mọi lệnh còn lại in JSON, kể cả guide và show, để agent đọc được từng trường một cách tường minh thay vì phải đoán ý từ văn bản.
- Ngoại lệ: help và config edit in chữ. Động từ chuyển tiếp in đúng những gì plugin in ra.

Mã thoát: 0 là thành công, 1 là lỗi người dùng hoặc dữ liệu, 2 là sai cú pháp lệnh. Khi lỗi, tlg in đối tượng lỗi ra stderr, stdout để trống. Động từ chuyển tiếp trả nguyên mã thoát của plugin. Schema của mọi đầu ra nằm ở mục Lệnh tlg trả ra những gì.

## JSON viết theo quy ước nào?

Áp cho cả giao thức core-plugin lẫn đầu ra của tlg.

- Định dạng sẵn (prettified): thụt 2 dấu cách, kết thúc bằng một dấu xuống dòng.
- UTF-8 nguyên văn. Không escape chữ có dấu thành \uXXXX, không escape các ký tự <, >, &.
- Gốc luôn là một object, không bao giờ là mảng hay giá trị đơn. Lý do: thêm trường mới vào gốc sau này không làm vỡ người đọc cũ.
- Tên trường là snake_case tiếng Anh.
- Thời điểm theo RFC 3339 kèm offset. Ngày không kèm giờ thì dạng YYYY-MM-DD. Core sinh thời điểm theo một múi giờ cố định (bản 1 là +07:00).
- Mọi trường có trong schema luôn có mặt, không bao giờ bị bỏ. Không có giá trị thì: chuỗi, số, object là null; mảng là []. Lý do: agent đọc đầu ra không phải phân biệt "thiếu trường" với "trường trống".
- Người đọc bỏ qua trường không có trong schema.

Ký hiệu kiểu trong các bảng schema: string, int, bool, string? (string hoặc null), T[] (mảng phần tử kiểu T). Kiểu là tên một bản ghi định nghĩa trong tài liệu này thì viết đúng tên đó, ví dụ Task, Session.

## Core nói chuyện với plugin thế nào?

Mỗi lần gọi, core chạy chương trình plugin với một tham số là tên động từ, gửi một object JSON qua stdin, và đọc một object JSON từ stdout. Plugin in log ra stderr. Mã thoát khác 0 nghĩa là plugin lỗi, core lấy dòng cuối của stderr làm lý do. Từ chối có chủ đích (log không qua kiểm tra, locate không thấy task) không phải lỗi: plugin trả mã thoát 0 và nói rõ trong bản ghi trả về.

Core chạy plugin với các biến môi trường khai ở cấu hình của plugin đó, cộng thêm môi trường của chính tlg.

Động từ plugin bắt buộc hỗ trợ: hello, new, status, show, log. Động từ không bắt buộc, plugin khai trong hello: guide, locate, pull, run.

### Core gửi gì?

Mọi động từ dùng chung một bản ghi:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| protocol | int | Phiên bản giao thức core đang dùng cho lần gọi này |
| verb | string | Tên động từ, trùng với tham số dòng lệnh |
| task | TaskRef? | Task được nhắm tới. null với hello, new và locate |
| args | string[] | Tham số nguyên văn người dùng gõ sau động từ |
| input | object? | Dữ liệu riêng của động từ, xem bảng dưới. null khi động từ không cần |

TaskRef:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| id | string | Task id |
| locator | string | Chuỗi định vị đã lưu trong sổ |

input theo động từ:

| Động từ | Trường của input |
|---|---|
| new | id: string? (null khi người dùng không truyền, plugin tự đề xuất); title: string; parent: TaskRef? (chỉ có khi tạo con cùng plugin với cha) |
| locate | id: string |
| guide | topic: string? (null là phần mặc định của giai đoạn hiện tại); status: Status (bản ghi status mới nhất của task) |
| còn lại | null |

Ví dụ một lần gọi status:

```json
{
  "protocol": 1,
  "verb": "status",
  "task": {
    "id": "WRK-101",
    "locator": "2026/WRK-101"
  },
  "args": [],
  "input": null
}
```

### Plugin trả gì?

hello:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| protocol | int | Phiên bản giao thức plugin hỗ trợ |
| verbs | string[] | Mọi động từ plugin hỗ trợ, gồm cả hello |
| children | bool | Plugin có nhận tạo task con không |

new:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| id | string | Id cuối cùng của task: id người dùng truyền, hoặc id plugin đề xuất |
| locator | string | Chuỗi định vị, core lưu vào sổ |
| title | string | Tên task |

new bị từ chối (ví dụ plugin không nhận tạo con, thiếu tham số) thì plugin trả mã thoát khác 0 kèm lý do trên stderr. Core kiểm tra id trùng và luật ký tự cả trước lẫn sau khi gọi plugin, vì plugin có thể đề xuất id.

status, gọi là bản ghi Status:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| id | string | Task id |
| title | string | Tên task |
| state | string | Một trong needs-me, active, waiting, paused, closed. Giá trị khác thì core coi như plugin lỗi |
| stage | string | Nhãn giai đoạn tự do, core in nguyên văn. Ví dụ "S7 gate failed" |
| last_activity | string? | Thời điểm hoạt động cuối. null khi task chưa có lịch sử |
| next | string | Việc tiếp theo, một dòng. Chuỗi rỗng khi không có |
| due | string? | Hạn, dạng YYYY-MM-DD. Core gắn cờ overdue khi quá hạn mà task chưa closed |
| flags | string[] | Cờ của plugin, core in nguyên văn |
| tags | string[] | Nhãn plugin góp, chỉ để đọc. Bản 1 luôn là [] |

Ví dụ:

```json
{
  "id": "WRK-103",
  "title": "Nâng cấp thư viện",
  "state": "needs-me",
  "stage": "S7 gate failed",
  "last_activity": "2026-11-18T14:36:00+07:00",
  "next": "quyết làm lại hay bỏ",
  "due": "2026-12-05",
  "flags": [
    "ticket In-progress"
  ],
  "tags": []
}
```

show, gọi là bản ghi Show: mọi trường của Status, cộng:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| summary | string | Đoạn báo đầu phiên, 1 tới 3 câu: đang ở đâu, việc tiếp theo |
| pending | string[] | Những gì còn thiếu để đi tiếp, mỗi phần tử một việc, ví dụ phần cửa chưa qua. [] khi không thiếu gì |
| recent | HistoryEntry[] | Vài dòng lịch sử cuối, tối đa 3. Không dựng lại cả lịch sử |

HistoryEntry:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| time | string | Thời điểm của dòng lịch sử |
| stage | string? | Giai đoạn của dòng. null với plugin không có giai đoạn |
| event | string | Từ sự kiện |
| note | string | Nội dung |

log:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| ok | bool | Đã ghi hay chưa |
| reason | string? | Lý do từ chối khi ok là false, kèm cách sửa. null khi ok là true |
| entry | HistoryEntry? | Dòng vừa ghi. null khi ok là false |

Tham số của log hoàn toàn do plugin định nghĩa. Plugin có giai đoạn có thể đòi log {giai đoạn} {sự kiện} {nội dung}, plugin ghi chép thì chỉ log {sự kiện} {nội dung}. Agent học cú pháp đó qua guide của plugin.

guide, gọi là bản ghi Guide. Hướng dẫn của core dùng chung bản ghi này:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| topic | string? | Chủ đề đang in. null là phần mặc định |
| rules | string[] | Quy tắc bắt buộc áp ngay, mỗi phần tử một quy tắc |
| sections | GuideSection[] | Nội dung chi tiết của chủ đề. [] ở phần mặc định khi rules đã đủ |
| refs | GuideRef[] | Skill, lệnh, file cần dùng, kèm khi nào dùng |
| topics | GuideTopic[] | Các chủ đề khác đọc được bằng guide {chủ đề} |

GuideSection: title: string; items: string[].

GuideRef: kind: string, một trong skill, command, file, url; name: string; when: string (khi nào cần dùng).

GuideTopic: name: string; when: string (khi nào cần đọc chủ đề này).

locate:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| found | bool | Plugin có thấy task không |
| locator | string? | Chuỗi định vị. null khi found là false |
| title | string? | Tên task. null khi found là false |

pull:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| ok | bool | Kéo thành công hay không |
| error | string? | Lý do khi ok là false |
| changed | string[] | Những gì đổi so với lần kéo trước. [] khi không đổi |

run: động từ dành cho mọi lệnh chuyển tiếp. Core gọi run với args là động từ lạ cùng các tham số sau nó, ví dụ tlg WRK-101 ticket diff thành run với args ["ticket", "diff"]. run không theo giao thức JSON ở chiều trả về: stdout và stderr của plugin được chuyển thẳng ra terminal, mã thoát được trả nguyên cho người gọi.

### Phiên bản và giới hạn thời gian

- Phiên bản là một số nguyên, bản 1 là 1. Thêm trường hay động từ không bắt buộc thì giữ số. Đổi nghĩa hay bỏ trường thì tăng số.
- Core nhận mọi phiên bản từ số nhỏ nhất nó còn hỗ trợ tới số mới nhất. Ngoài khoảng đó thì task của plugin mang cờ plugin needs update, board vẫn dựng, còn lệnh nhắm vào task trả lỗi protocol_mismatch.
- Core lưu kết quả hello vào bảng hello_cache, và chỉ hỏi lại khi đường dẫn hoặc thời điểm sửa đổi của file chương trình plugin đổi. doctor và plugin add luôn hỏi lại.

Mỗi lần gọi có giới hạn thời gian, mặc định 15 giây, chỉnh theo từng plugin trong cấu hình. Riêng run không giới hạn.

## Lệnh tlg trả ra những gì?

Các bản ghi dùng chung:

Task, một dòng của sổ:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| id | string | Task id |
| plugin | string | Tên plugin trong cấu hình |
| title | string | Tên task, bản sao lần status gần nhất |
| registry | string | tracked hoặc archived |
| parent_id | string? | Id task cha |
| created_at | string | Thời điểm thêm vào sổ |

Session:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| agent | string | claude, opencode, agy, hoặc tên người dùng truyền vào attach |
| session_id | string | Session id của agent |
| cwd | string | Thư mục làm việc |
| title | string | Tiêu đề phiên, chuỗi rỗng khi chưa có |
| started_at | string | Lần đầu phiên được gắn vào task |
| last_seen | string | Lần gần nhất phiên gọi tlg trên task |

Đối tượng lỗi, in ra stderr:

```json
{
  "error": {
    "code": "duplicate_id",
    "message": "task id WRK-101 already exists",
    "hint": "use another id or run: tlg WRK-101 show",
    "task_id": "WRK-101"
  }
}
```

| Trường | Kiểu | Nghĩa |
|---|---|---|
| code | string | Mã lỗi cố định, agent rẽ nhánh theo trường này |
| message | string | Lý do, một câu tiếng Anh |
| hint | string? | Cách sửa |
| task_id | string? | Task liên quan |

Mã lỗi:

| code | Khi nào | Mã thoát |
|---|---|---|
| usage | Sai cú pháp lệnh | 2 |
| task_not_found | Không có task id trong sổ | 1 |
| duplicate_id | Task id đã có trong sổ | 1 |
| invalid_id | Task id sai luật ký tự hoặc trùng động từ của core | 1 |
| cycle | Đặt cha tạo vòng lặp | 1 |
| plugin_not_found | Cấu hình không có plugin của task, hoặc không chạy được chương trình | 1 |
| plugin_error | Plugin trả mã thoát khác 0 hoặc bản ghi sai schema | 1 |
| plugin_timeout | Plugin quá giới hạn thời gian | 1 |
| protocol_mismatch | Plugin dùng phiên bản giao thức core không nhận | 1 |
| verb_unsupported | Plugin không hỗ trợ động từ được gọi | 1 |
| log_rejected | Plugin từ chối dòng log, message là lý do của plugin | 1 |
| not_located | locate không thấy task | 1 |
| config_invalid | File cấu hình sai, hoặc plugin không qua kiểm tra khi thêm | 1 |
| internal | Lỗi của chính tlg, ví dụ không mở được sổ | 1 |

Board là chỗ duy nhất chịu được plugin lỗi: lỗi của một task thành cờ trên dòng đó. Mọi lệnh nhắm vào một task thì trả đối tượng lỗi.

Đầu ra theo lệnh:

| Lệnh | Các trường ở gốc |
|---|---|
| ls | tasks: Task[] |
| new, track | task: Task; warnings: string[] |
| {id} archive, unarchive, parent | task: Task; warnings: string[]. archive một task chưa closed có cảnh báo trong warnings |
| {id} forget | id: string; sessions_removed: int |
| {id} attach | task: Task; session: Session |
| {id} sessions | task_id: string; sessions: Session[], phiên gần nhất đứng đầu |
| {id} show | task: Task; show: Show; children: string[] (id các con tracked); sessions: Session[] (tối đa 3 phiên gần nhất); flags: string[] (cờ của core) |
| {id} log | task_id: string; entry: HistoryEntry |
| guide | scope: "core"; task_id: null; cùng các trường của Guide |
| {id} guide | scope: "plugin"; task_id: string; cùng các trường của Guide |
| plugin ls, doctor | plugins: PluginCheck[]; orphan_tasks: string[] (id task có plugin không còn trong cấu hình); ok: bool |
| plugin add | plugin: PluginCheck |
| plugin rm | name: string |
| board --json | generated_at: string; groups: BoardGroup[] |

PluginCheck:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| name | string | Tên plugin |
| command | string | Đường dẫn chương trình |
| ok | bool | hello chạy được và đúng giao thức |
| protocol | int? | Phiên bản plugin trả về |
| verbs | string[] | Động từ plugin khai |
| children | bool? | Plugin có nhận task con không |
| error | string? | Lý do khi ok là false |

BoardGroup: state: string (needs-me, active, waiting, paused, closed); trees: BoardNode[]. Nhóm không có task nào thì không có mặt.

BoardNode:

| Trường | Kiểu | Nghĩa |
|---|---|---|
| id | string | Task id |
| plugin | string | Tên plugin |
| title | string | Tên task |
| state | string? | Trạng thái riêng của task. null khi không lấy được trạng thái |
| stage | string | Nhãn giai đoạn. Chuỗi rỗng khi không lấy được trạng thái |
| last_activity | string? | Thời điểm hoạt động cuối |
| idle_days | int? | Số ngày không động |
| next | string | Việc tiếp theo |
| due | string? | Hạn |
| marked | bool | Trạng thái riêng đúng bằng nhóm của cây, tức dòng có dấu * trên bảng chữ |
| flags | string[] | Cờ của plugin rồi tới cờ của core |
| tags | string[] | Nhãn |
| children | BoardNode[] | Các con, theo đúng thứ tự trên bảng chữ |

## Cấu hình viết thế nào?

```toml
[registry]
path = "~/.local/share/taskledger/registry.db"

[plugins.work]
command = "~/bin/taskledger-plugin-work"
env = { WORK_ROOT = "~/work/tasks" }
timeout = "15s"

[plugins.notes]
command = "~/bin/taskledger-plugin-personal"
env = { PERSONAL_ROOT = "~/notes/tasks" }
```

- Tên plugin là khoá trong cấu hình, và là giá trị cột plugin trong sổ: chữ thường, số, gạch ngang. Đổi đường dẫn chương trình không đụng tới sổ.
- Key lạ trong file cấu hình là lỗi, để gõ sai tên key không bị bỏ qua im lặng.
- Biến môi trường TLG_CONFIG trỏ sang một file cấu hình khác. Dùng nó để chạy thử trên một bộ dữ liệu riêng, với sổ riêng, mà không đụng tới bộ thật.
- Nên để chuỗi định vị là đường dẫn tương đối so với một thư mục gốc khai ở env. Chuyển plugin sang bộ dữ liệu khác khi đó chỉ là đổi một dòng cấu hình.
- config edit mở $VISUAL hoặc $EDITOR trên một bản sao. Đóng editor thì core kiểm tra bản sao: cú pháp TOML, mỗi plugin chạy được và trả lời hello đúng giao thức. Đạt thì thay file thật, không đạt thì in lỗi và hỏi mở lại hay bỏ. Đây là cách visudo làm với sudoers.
- plugin add, plugin rm, plugin ls dành cho agent, không mở editor. plugin add kiểm tra plugin giống config edit trước khi ghi.
- doctor chạy hello cho từng plugin, báo plugin nào không chạy được hoặc trả sai giao thức, và báo task nào trong sổ thuộc plugin không còn trong cấu hình.

## Phiên agent được ghi nhận thế nào?

Danh sách phiên của một task dùng để hiển thị và để mở tiếp. Ở bản 1, người dùng tự mở tiếp: lệnh sessions in agent, session id, thư mục làm việc, thời điểm, đủ để gõ lệnh resume của agent đó.

Gắn tự động: mỗi khi được gọi bằng một lệnh nhắm vào đúng một task (new, show, log, guide của task, động từ chuyển tiếp), core đọc biến môi trường để nhận ra agent và session id. Có rồi thì cập nhật last_seen, chưa có thì thêm. Lệnh chung (board, ls) và các lệnh quản lý sổ (archive, parent, sessions) không gắn phiên, vì một phiên có thể hỏi board rồi mới chọn task.

Bảng nhận diện agent nằm trong core, vì nó không liên quan tới nghiệp vụ:

| Agent | Nhận ra bằng | Session id lấy từ | Trạng thái |
|---|---|---|---|
| claude | AI_AGENT bắt đầu bằng claude-code | CLAUDE_CODE_SESSION_ID | Đã xác minh trên Claude Code 2.1 |
| opencode | | | Chưa xác minh, bản 2 |
| agy | | | Chưa xác minh, bản 2 |

Thư mục làm việc lấy từ thư mục hiện tại của tiến trình tlg. Gắn tự động để title trống, gắn lại không xoá title đã có.

Gắn tay bằng attach, dùng cho agent chưa nhận diện được và cho phiên có trước khi dùng tlg.

## Agent học cách dùng tlg từ đâu?

Hướng dẫn do chính tlg in ra, để chỉ có một nguồn và hướng dẫn đổi theo bản binary:

| Lớp | Nội dung | Ai giữ |
|---|---|---|
| tlg guide | Cách dùng core | Core |
| tlg {id} guide | Hướng dẫn của plugin cho task đó | Plugin |
| Vỏ cho từng agent | Vài dòng: trước khi làm việc với task, chạy tlg guide và tlg {id} guide | Skill hoặc file quy tắc của agent |

Quy tắc chung: agent cần đến đâu thì in đến đó, không in thừa.

- tlg guide trả phần mặc định: rules là các quy tắc bắt buộc (luôn có task id, ghi lịch sử qua log, đầu phiên đọc show, sau board thì đề xuất tối đa ba việc nên làm tiếp kèm lý do, không chạy config edit), topics là danh sách chủ đề kèm khi nào cần đọc, sections để trống. tlg guide {chủ đề} trả chi tiết trong sections. Chủ đề của core: commands, board, sessions, tasks, errors.
- tlg {id} guide trả phần của giai đoạn hiện tại, không trả các giai đoạn khác. refs nêu skill và công cụ của giai đoạn đó. tlg {id} guide {chủ đề} trả chi tiết. Core gọi status trước rồi truyền kết quả vào input của guide, để plugin không phải tự đọc lại trạng thái.
- show trả đoạn báo đầu phiên trong summary và pending, tối đa 3 dòng lịch sử trong recent. Không dựng lại lịch sử.
- Lỗi là một đối tượng lỗi có mã cố định, không kèm hướng dẫn dài.
- Vỏ cho agent không chép lại nội dung guide.

## Viết một plugin thế nào?

Một plugin tối thiểu cần:

1. Nhận tên động từ ở tham số đầu, đọc request JSON từ stdin.
2. Trả hello với protocol 1, danh sách động từ, và children.
3. Thực hiện new, status, show, log theo schema ở mục trên. Mọi trường phải có mặt.
4. Từ chối có chủ đích bằng bản ghi (log trả ok false, locate trả found false), lỗi thật bằng mã thoát khác 0 và một dòng lý do cuối stderr.
5. Không chờ bàn phím, không in gì khác JSON ra stdout (trừ run).

Plugin quyết toàn bộ phần nghiệp vụ: task lưu ở đâu, lịch sử có những sự kiện gì, sự kiện nào ra trạng thái nào. Plugin có quy trình nhiều giai đoạn thường đặt nhãn giai đoạn vào stage, phần cửa chưa qua vào pending của show, và hướng dẫn của giai đoạn hiện tại vào guide. Plugin kéo dữ liệu từ nguồn ngoài (issue tracker, ticket) thì thực hiện pull và đưa trạng thái ngoài vào flags.

Plugin có thể là lớp mỏng bọc một bộ script đã có. Khi đó plugin chỉ nói giao thức và ánh xạ khái niệm của bộ script sang 5 trạng thái, còn việc kiểm tra và ghi dữ liệu vẫn do bộ script làm, để hai bên không thể lệch nhau.

## Plugin ghi chép cá nhân

Repo taskledger-plugin-personal là plugin mẫu, dành cho việc học, dự án cá nhân, mục tiêu cá nhân. Nó cũng là bằng chứng giao thức không dính giả định của một quy trình cụ thể (nhiều giai đoạn, id lấy từ ticket). Chỗ nào phải làm trái giao thức mới viết được plugin này thì đó là lỗi của giao thức, sửa giao thức chứ không lách trong plugin.

| Điểm | Thiết kế |
|---|---|
| Giai đoạn, cửa | Không có |
| Lưu trữ | Mỗi task một thư mục {PERSONAL_ROOT}/{task id}/, gồm history.md và notes.md. Chuỗi định vị là task id |
| history.md | Bảng Markdown chỉ ghi thêm, cột Time, Event, Note |
| Bộ từ sự kiện | start, note, next, wait, pause, resume, close |
| Lệnh log | log {event} {note}. Kiểm tra từ sự kiện, nội dung tối đa 160 ký tự, một dòng, không chứa dấu |. Sự kiện wait phải có dạng "{ai}: {gì}". Không qua kiểm tra thì từ chối, không ghi gì |
| Trạng thái từ dòng cuối | wait: waiting; pause: paused; close: closed; còn lại: active. History rỗng thì needs-me kèm cờ history empty |
| next trên board | Nội dung của dòng next gần nhất. Dòng cuối là wait thì là nội dung dòng đó |
| stage | Từ sự kiện của dòng cuối |
| show | Tên, trạng thái, việc tiếp theo, 3 dòng lịch sử cuối |
| new | Không nhận tham số riêng. Không có id thì đề xuất ME_{YYYYMMDD}_{NNN}, NNN tăng dần trong ngày. Tạo thư mục, ghi dòng start |
| Con | Hỗ trợ (children true). Con là một task bình thường |
| guide | Phần mặc định là luật ghi log, chủ đề events giải thích từng từ sự kiện |

## Bản 1 và bản 2 gồm những gì?

| Tính năng | Bản 1 | Bản 2 |
|---|---|---|
| Sổ SQLite, hello, new, status, show, log, locate, pull, chuyển tiếp lệnh | x | |
| board dạng cây, cờ, dấu *, --pull, --json | x | |
| ls, track, archive, unarchive, forget | x | |
| Phiên: tự gắn cho Claude Code, attach, sessions | x | |
| config edit, plugin add/rm/ls, doctor | x | |
| guide của core và của plugin | x | |
| Plugin ghi chép cá nhân | x | |
| Cha con, id con mặc định | x | |
| Phụ thuộc | | x |
| Nhãn | | x |
| board --flat | | x |
| Tự gắn phiên cho opencode, agy | | x |

## Bản 2 sẽ thêm phụ thuộc và nhãn ra sao?

Hai tính năng này để bản 2 nhưng đã chốt thiết kế, để schema và giao thức bản 1 không phải đổi.

Phụ thuộc (task A bị chặn bởi task B):

- Lưu ở core, một bảng riêng. Nhiều-nhiều, được khác plugin, core chặn vòng lặp.
- Người dùng, hoặc agent theo lời người dùng, thêm và gỡ bằng lệnh của core. Plugin không tự thêm.
- Không đổi trạng thái hay nhóm của A. A mang cờ blocked by B. Khi mọi task chặn A đã closed, cờ đổi thành unblocked.
- Không vẽ lên cây, cây chỉ dành cho cha con.

Nhãn:

- Lưu ở core, người dùng đặt bằng lệnh của core. Chỉ gồm chữ thường không dấu, số, gạch ngang.
- Plugin góp nhãn qua trường tags của status. Nhãn này chỉ để đọc, lấy mới mỗi lần dựng board, không ghi vào sổ, để sổ không lệch khi nguồn ngoài đổi.
- Không lưu kế thừa. Nhưng lọc theo nhãn thì một task khớp kéo theo cả nhánh con của nó, và ở dạng cây thì kèm đường đi lên tới gốc.
- Hiện cuối dòng board, cùng chỗ với cờ.

## Còn gì chưa chốt?

| Điểm | Cần làm |
|---|---|
| Biến môi trường của opencode, agy | Xác minh mỗi agent có biến nào chứa tên agent và session id |
| Định dạng file quy tắc của agy | Xác minh agy đọc hướng dẫn từ đâu |
| Title của phiên tự gắn | Gắn tự động để trống. Chưa có nguồn lấy tiêu đề phiên |
| Múi giờ | Bản 1 cố định +07:00. Cần một key cấu hình nếu có người dùng ở múi giờ khác |
