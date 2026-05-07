---
name: clean-code
description: Clean Code principles for writing readable, maintainable, and professional software. Use this skill when writing, reviewing, or refactoring code in any language. Triggers on tasks involving code generation, code review, refactoring, naming conventions, function design, error handling, test writing, or general code quality improvement. Optimized for Java and Python.
license: MIT
metadata:
  author: community
  version: "1.0.0"
  inspired_by: "Clean Code principles (Robert C. Martin)"
---

# Clean Code Skill

Practical guidelines for writing clean, readable, and maintainable code. Contains 52 rules across 8 categories, prioritized by impact on code quality. Language-agnostic principles with Java and Python examples.

## When to Apply

Reference these guidelines when:
- Writing new functions, classes, or modules
- Reviewing code for readability and maintainability
- Refactoring existing code
- Designing error handling strategies
- Writing or improving unit tests
- Naming variables, functions, classes, or packages
- Deciding whether to add or remove comments

## Rule Categories by Priority

| Priority | Category | Impact | Prefix |
|----------|----------|--------|--------|
| 1 | Naming | CRITICAL | `naming-` |
| 2 | Functions | CRITICAL | `fn-` |
| 3 | Error Handling | HIGH | `err-` |
| 4 | Testing | HIGH | `test-` |
| 5 | Classes & Objects | MEDIUM-HIGH | `class-` |
| 6 | Comments | MEDIUM | `comment-` |
| 7 | Formatting | MEDIUM | `format-` |
| 8 | Boundaries | MEDIUM | `boundary-` |

## Quick Reference

### 1. Naming (CRITICAL)

- `naming-intention` - Names should reveal intent without requiring a comment
- `naming-no-disinformation` - Avoid names that lie about what something is or does
- `naming-distinction` - Make meaningful distinctions, never use noise words
- `naming-pronounceable` - Use pronounceable names you can discuss verbally
- `naming-searchable` - Use names that are easy to grep for, avoid single letters
- `naming-no-encoding` - Don't encode types or scope into names
- `naming-class-noun` - Classes are nouns, never verbs
- `naming-method-verb` - Methods are verbs or verb phrases
- `naming-one-word-per-concept` - Use one word per abstract concept consistently
- `naming-domain-terms` - Use solution domain and problem domain terms appropriately

### 2. Functions (CRITICAL)

- `fn-small` - Functions should be small, then smaller than that
- `fn-one-thing` - A function should do one thing and do it well
- `fn-one-level-of-abstraction` - Statements in a function should be at one level of abstraction
- `fn-args-minimal` - Fewer arguments are better, three is the practical max
- `fn-no-flag-args` - Never pass a boolean to select behavior, split into two functions
- `fn-no-side-effects` - A function should not have hidden side effects
- `fn-command-query-separation` - A function should either do something or answer something, not both
- `fn-prefer-exceptions` - Use exceptions over returning error codes
- `fn-extract-try-catch` - Extract try/catch bodies into their own functions
- `fn-dry` - Don't repeat yourself, duplication is the root of all evil in software

### 3. Error Handling (HIGH)

- `err-exceptions-over-codes` - Use exceptions instead of return codes for error signaling
- `err-unchecked-exceptions` - Prefer unchecked exceptions to avoid dependency chains
- `err-context-in-exceptions` - Provide enough context in exception messages to locate the failure
- `err-caller-defined-exceptions` - Define exception classes by how the caller needs to handle them
- `err-no-null-return` - Don't return null, use Optional, empty collections, or Special Case pattern
- `err-no-null-pass` - Don't pass null as an argument unless the API explicitly requires it
- `err-wrap-third-party` - Wrap third-party APIs to normalize their exception behavior

### 4. Testing (HIGH)

- `test-one-assert-per-concept` - Test one concept per test, not necessarily one assert
- `test-fast` - Tests must run fast or developers will stop running them
- `test-independent` - Tests should not depend on each other or on ordering
- `test-repeatable` - Tests must produce the same result in any environment
- `test-self-validating` - Tests should return boolean pass/fail, no manual inspection
- `test-timely` - Write tests just before the production code that makes them pass
- `test-clean-tests` - Test code deserves the same quality as production code
- `test-readable` - Tests should read as clear specifications of behavior
- `test-build-operate-check` - Structure tests as build, operate, check (Arrange, Act, Assert)
- `test-domain-language` - Build a domain-specific testing language for readability

### 5. Classes & Objects (MEDIUM-HIGH)

- `class-small` - Classes should be small, measured by responsibility not line count
- `class-single-responsibility` - A class should have one and only one reason to change
- `class-cohesion` - Methods should manipulate a high proportion of the class variables
- `class-organize-for-change` - Structure classes so changes are isolated and low-risk
- `class-prefer-polymorphism` - Prefer polymorphism over switch/if-else type checking
- `class-law-of-demeter` - A method should only talk to its immediate friends
- `class-data-transfer-objects` - Use DTOs for data transfer, keep them free of business logic

### 6. Comments (MEDIUM)

- `comment-why-not-what` - Comments should explain why, never what the code does
- `comment-no-journal` - Don't keep changelogs in comments, that's what git is for
- `comment-no-noise` - Remove comments that restate the obvious
- `comment-no-closing-brace` - Don't comment closing braces, shorten the function instead
- `comment-no-commented-out-code` - Delete commented-out code, version control remembers
- `comment-legal-headers` - Legal headers are acceptable when required
- `comment-todo` - TODO comments are acceptable but must be tracked and resolved
- `comment-warn-consequences` - Warning comments about non-obvious consequences are valuable

### 7. Formatting (MEDIUM)

- `format-vertical-density` - Related code should appear vertically dense
- `format-vertical-distance` - Variables declared close to their usage
- `format-dependent-functions` - Caller above callee, high-level above low-level
- `format-newspaper-rule` - File reads top-to-bottom like a newspaper article
- `format-team-rules` - The team agrees on one set of rules and everyone follows them
- `format-horizontal-limit` - Lines should be short enough to read without scrolling

### 8. Boundaries (MEDIUM)

- `boundary-wrap-third-party` - Wrap third-party APIs behind your own interfaces
- `boundary-learning-tests` - Write tests against third-party code to learn and detect changes
- `boundary-adapter-pattern` - Use adapters when integrating code that doesn't exist yet



## How to Use

Read individual rule files for detailed explanations and code examples:

```
rules/naming-intention.md
rules/fn-small.md
```

Each rule file contains:
- Brief explanation of why it matters
- Bad code example with explanation
- Clean code example with explanation
- Language-specific notes for Java and Python

## Full Compiled Document

For the complete guide with all rules expanded: `AGENTS.md`
-e 

---

# Full Rule Reference

-e 
---

# naming-class-noun

## Classes Are Nouns

Class names should be nouns or noun phrases. They represent things, not actions. Avoid vague names like `Manager`, `Processor`, `Data`, or `Info` that don't describe what the class actually is.

### Bad

```java
class ProcessData { }
class WebsiteManager { }
class HandlePayment { }
class Misc { }
```

### Clean

```java
class Customer { }
class PaymentGateway { }
class WikiPage { }
class AddressParser { }
```

```python
# Bad
class DoStuff: pass
class ManageThings: pass

# Clean
class Account: pass
class TransactionLedger: pass
```

### Why It Matters

A class is a blueprint for an object — a thing that exists. Naming it as a verb or vague abstraction makes it impossible to reason about what it contains and what it's responsible for. If you can't come up with a crisp noun, the class is probably doing too much.

-e 
---

# naming-distinction

## Make Meaningful Distinctions

If names must be different, they should also mean something different. Noise words like `Info`, `Data`, `Manager`, `Processor`, `a1/a2` are not distinctions.

### Bad

```java
public static void copyChars(char a1[], char a2[]) {
    for (int i = 0; i < a1.length; i++) {
        a2[i] = a1[i];
    }
}

// What's the difference between these?
class ProductInfo { }
class ProductData { }

String nameString;  // as opposed to nameInteger?
```

### Clean

```java
public static void copyChars(char[] source, char[] destination) {
    for (int i = 0; i < source.length; i++) {
        destination[i] = source[i];
    }
}

class Product { }

String name;
```

```python
# Bad
def copy(a1, a2):
    for i in range(len(a1)):
        a2[i] = a1[i]

# Clean
def copy_chars(source: list, destination: list):
    for i in range(len(source)):
        destination[i] = source[i]
```

### Why It Matters

Noise words are redundant distinctions that force the reader to understand the difference by reading the implementation rather than the name. If you can't tell what two things are from their names alone, the names have failed.

-e 
---

# naming-domain-terms

## Use Solution Domain and Problem Domain Terms

Use computer science terms (pattern names, algorithm names, data structure names) when the concept is from the solution domain. Use business/problem domain terms when the concept is from the business.

### Bad

```java
// Generic names that don't leverage either domain
class DataHandler {
    private List<Object> stuff;
    public void process(Object thing) { }
}
```

### Clean

```java
// Solution domain: CS terms for technical concepts
class JobQueue {
    private final BlockingDeque<Runnable> workItems;
    public void enqueue(Runnable task) { }
}

// Problem domain: business terms for business concepts
class LoanApplication {
    private CreditScore borrowerCredit;
    private Money requestedPrincipal;
    public UnderwritingDecision evaluate() { }
}
```

```python
# Solution domain
class LRUCache:
    def evict_oldest(self) -> None: ...

# Problem domain
class ShippingManifest:
    def calculate_duties(self, destination: Country) -> Money: ...
```

### Why It Matters

When a programmer reads `AccountVisitor`, they recognize the Visitor pattern and immediately understand the structure. When a domain expert reads `LoanApplication.evaluate()`, they understand the business process. Using the right vocabulary for the right audience makes the code accessible to whoever needs to read it.

-e 
---

# naming-intention

## Names Should Reveal Intent

A name should tell you why it exists, what it does, and how it is used. If a name requires a comment to explain it, the name is wrong.

### Bad

```java
int d; // elapsed time in days
List<int[]> list1;

public List<int[]> getThem() {
    List<int[]> list1 = new ArrayList<>();
    for (int[] x : theList)
        if (x[0] == 4)
            list1.add(x);
    return list1;
}
```

### Clean

```java
int elapsedTimeInDays;
List<Cell> flaggedCells;

public List<Cell> getFlaggedCells() {
    List<Cell> flaggedCells = new ArrayList<>();
    for (Cell cell : gameBoard)
        if (cell.isFlagged())
            flaggedCells.add(cell);
    return flaggedCells;
}
```

```python
# Bad
d = 0
l1 = [x for x in the_list if x[0] == 4]

# Clean
elapsed_time_in_days = 0
flagged_cells = [cell for cell in game_board if cell.is_flagged()]
```

### Why It Matters

The cost of reading code far exceeds the cost of writing it. A well-named variable eliminates the need for the reader to track mental context about what cryptic symbols mean. Every name is a missed opportunity to communicate or a seized one.

-e 
---

# naming-method-verb

## Methods Are Verbs

Method names should be verbs or verb phrases. They do things. Accessors, mutators, and predicates should follow the `get`, `set`, `is` convention.

### Bad

```java
class Account {
    public Money balance() { }     // Is this getting or calculating?
    public void active(boolean a) { } // Is this checking or setting?
    public String data() { }       // verb or noun?
}
```

### Clean

```java
class Account {
    public Money getBalance() { }
    public void setActive(boolean active) { }
    public boolean isActive() { }
    public String serialize() { }
    public void deposit(Money amount) { }
}
```

```python
class Account:
    # Python uses properties for get/set but methods are still verbs
    @property
    def balance(self) -> Money:
        return self._balance

    def deposit(self, amount: Money) -> None: ...
    def withdraw(self, amount: Money) -> None: ...
    def is_overdrawn(self) -> bool: ...
```

### Why It Matters

When methods read as verbs, code reads like prose: `account.deposit(payment)`, `order.cancel()`, `user.is_active()`. When they don't, every call site forces the reader to guess what action is being performed.

### Note

Python is more flexible here — properties can act as noun-like accessors (`account.balance`) and that's idiomatic. The key principle is that actions should be clearly verb-named.

-e 
---

# naming-no-disinformation

## Avoid Names That Lie

Don't use names that imply something the code doesn't deliver. A variable called `accountList` that isn't a `List` is disinformation. A name that looks like another well-known name but means something different causes bugs.

### Bad

```java
// Not actually a List, it's a Map
Map<String, Account> accountList;

// Too similar, easy to confuse
XYZControllerForEfficientHandlingOfStrings
XYZControllerForEfficientStorageOfStrings

// Lowercase L and O look like 1 and 0
int a = l;
if (O == l) a = O1;
```

### Clean

```java
Map<String, Account> accountsByName;

StringHandler stringHandler;
StringStorage stringStorage;
```

```python
# Bad
account_list = {}  # it's a dict, not a list
hp = get_result()  # hp could mean hit points, horsepower, or health plan

# Clean
accounts_by_name = {}
health_plan = get_result()
```

### Why It Matters

Disinformative names create false mental models. The reader builds an assumption about what the code does based on the name, and that assumption is wrong. This is worse than a meaningless name because it actively misleads rather than just being unhelpful.

-e 
---

# naming-no-encoding

## Don't Encode Types or Scope Into Names

Hungarian notation, member prefixes (`m_`), and interface prefixes (`I`) add visual clutter without adding information. Modern IDEs and type systems make these encodings redundant.

### Bad

```java
// Hungarian notation
String strName;
int iCount;
boolean bIsActive;

// Member prefix
private String m_description;

// Interface prefix
interface IShapeFactory { }
class ShapeFactory implements IShapeFactory { }
```

### Clean

```java
String name;
int count;
boolean isActive;

private String description;

interface ShapeFactory { }
class ShapeFactoryImpl implements ShapeFactory { }
// Or better: name the implementation by what makes it specific
class CachedShapeFactory implements ShapeFactory { }
```

```python
# Bad - Python has no need for type encoding
str_name = "hello"
lst_items = []
dict_config = {}

# Clean - type hints serve this purpose
name: str = "hello"
items: list[Item] = []
config: dict[str, Any] = {}
```

### Why It Matters

Encoded names are harder to read and harder to change. If a variable changes type, you have to change the name everywhere or live with a lie. The encoding also trains your eye to skip the prefix, making it pure noise.

-e 
---

# naming-one-word-per-concept

## One Word Per Abstract Concept

Pick one word for one abstract concept and stick with it. Don't use `fetch`, `retrieve`, and `get` as equivalent methods across different classes. Don't use `controller`, `manager`, and `driver` interchangeably.

### Bad

```java
class UserRepository {
    public User fetchUser(int id) { }
}

class OrderRepository {
    public Order retrieveOrder(int id) { }
}

class ProductRepository {
    public Product getProduct(int id) { }
}
```

### Clean

```java
class UserRepository {
    public User findById(int id) { }
}

class OrderRepository {
    public Order findById(int id) { }
}

class ProductRepository {
    public Product findById(int id) { }
}
```

```python
# Bad - inconsistent naming across similar abstractions
class UserService:
    def fetch_user(self, id: int) -> User: ...

class OrderService:
    def retrieve_order(self, id: int) -> Order: ...

# Clean - consistent vocabulary
class UserService:
    def find_by_id(self, id: int) -> User: ...

class OrderService:
    def find_by_id(self, id: int) -> Order: ...
```

### Why It Matters

Inconsistent naming forces the developer to remember which synonym goes with which class. A consistent lexicon across the codebase means patterns are immediately recognizable and the developer spends zero mental energy on vocabulary differences that carry no semantic difference.

-e 
---

# naming-pronounceable

## Use Pronounceable Names

If you can't say it, you can't discuss it. Code is a social artifact — teams talk about code verbally. Unpronounceable names make those conversations awkward and error-prone.

### Bad

```java
class DtaRcrd102 {
    private Date genymdhms;
    private Date modymdhms;
    private final String pszqint = "102";
}
```

### Clean

```java
class Customer {
    private Date generationTimestamp;
    private Date modificationTimestamp;
    private final String recordId = "102";
}
```

```python
# Bad
gen_ymdhms = datetime.now()
mod_ymdhms = datetime.now()

# Clean
generation_timestamp = datetime.now()
modification_timestamp = datetime.now()
```

### Why It Matters

Programming is a collaborative activity. When a developer can't pronounce a name in a code review or standup, they resort to spelling it out or inventing nicknames. Both waste time and introduce ambiguity.

-e 
---

# naming-searchable

## Use Searchable Names

Single-letter names and numeric constants are impossible to grep for across a codebase. The length of a name should correspond to the size of its scope.

### Bad

```java
for (int j = 0; j < 34; j++) {
    s += (t[j] * 4) / 5;
}
```

### Clean

```java
int realDaysPerIdealDay = 4;
final int WORK_DAYS_PER_WEEK = 5;
int sum = 0;

for (int j = 0; j < NUMBER_OF_TASKS; j++) {
    int realTaskDays = taskEstimate[j] * realDaysPerIdealDay;
    int realTaskWeeks = realTaskDays / WORK_DAYS_PER_WEEK;
    sum += realTaskWeeks;
}
```

```python
# Bad
for j in range(34):
    s += (t[j] * 4) / 5

# Clean
WORK_DAYS_PER_WEEK = 5
REAL_DAYS_PER_IDEAL_DAY = 4

total_weeks = sum(
    (estimate * REAL_DAYS_PER_IDEAL_DAY) / WORK_DAYS_PER_WEEK
    for estimate in task_estimates
)
```

### Why It Matters

When you need to find every usage of a concept across a project, searchable names make it possible. Magic numbers and single letters bury meaning in noise and make refactoring dangerous because you can't reliably find all occurrences.

### Exception

Single-letter loop variables like `i` are acceptable in very short loops where the scope is immediately visible. Outside of that, name it.

-e 
---

# fn-args-minimal

## Fewer Arguments Are Better

Zero arguments is ideal, one is fine, two is acceptable, three should be avoided. More than three requires strong justification. Many arguments often signal the function is doing too much or the arguments should be wrapped in an object.

### Bad

```java
Circle makeCircle(double x, double y, double radius, Color color, boolean filled) { }
```

### Clean

```java
Circle makeCircle(Point center, double radius) { }
// or
Circle makeCircle(CircleSpec spec) { }
```

```python
# Bad
def create_user(name, email, age, role, department, manager_id, start_date):
    ...

# Clean
@dataclass
class UserRegistration:
    name: str
    email: str
    age: int
    role: str
    department: str
    manager_id: int
    start_date: date

def create_user(registration: UserRegistration) -> User:
    ...
```

### Why It Matters

Arguments increase the conceptual weight of a function. Every argument is something the reader must understand and remember. Arguments also make testing harder — each one multiplies the number of test combinations needed to cover the function.

-e 
---

# fn-command-query-separation

## Command-Query Separation

A function should either do something (command) or answer something (query), never both. When a function changes state and also returns a value, it creates confusion at the call site.

### Bad

```java
// Does this set the attribute and return success?
// Or does it check if the attribute exists?
public boolean set(String attribute, String value) { }

// Confusing call site
if (set("username", "admin")) { }
```

### Clean

```java
public boolean attributeExists(String attribute) { }  // query
public void setAttribute(String attribute, String value) { }  // command

if (attributeExists("username")) {
    setAttribute("username", "admin");
}
```

```python
# Bad
def set_and_check(key: str, value: str) -> bool:
    self.data[key] = value
    return key in self.required_fields

# Clean
def is_required_field(self, key: str) -> bool:
    return key in self.required_fields

def set_field(self, key: str, value: str) -> None:
    self.data[key] = value
```

### Why It Matters

When a function both mutates state and returns a value, the reader must understand the side effect to interpret the return value. Separating commands from queries means each function has a single clear purpose and the call site reads unambiguously.

### Exception

Builder patterns and fluent APIs intentionally return `this` from mutation methods. That's an accepted convention where the return value enables chaining, not inspection.

-e 
---

# fn-dry

## Don't Repeat Yourself

Duplication is the root of all evil in software. Every piece of knowledge should have a single, unambiguous representation. When you see the same code in two or more places, extract it.

### Bad

```java
public void printEmployeeReport(Employee employee) {
    System.out.println("Name: " + employee.getName());
    System.out.println("ID: " + employee.getId());
    System.out.println("Department: " + employee.getDepartment());
    System.out.println("--- Performance ---");
    System.out.println("Rating: " + employee.getRating());
}

public void printEmployeeSummary(Employee employee) {
    System.out.println("Name: " + employee.getName());
    System.out.println("ID: " + employee.getId());
    System.out.println("Department: " + employee.getDepartment());
    System.out.println("--- Summary ---");
    System.out.println("Years: " + employee.getTenure());
}
```

### Clean

```java
public void printEmployeeReport(Employee employee) {
    printEmployeeHeader(employee);
    System.out.println("--- Performance ---");
    System.out.println("Rating: " + employee.getRating());
}

public void printEmployeeSummary(Employee employee) {
    printEmployeeHeader(employee);
    System.out.println("--- Summary ---");
    System.out.println("Years: " + employee.getTenure());
}

private void printEmployeeHeader(Employee employee) {
    System.out.println("Name: " + employee.getName());
    System.out.println("ID: " + employee.getId());
    System.out.println("Department: " + employee.getDepartment());
}
```

```python
# The same principle applies to logic, not just code structure.
# Duplicated conditionals, duplicated validation, duplicated transformations
# are all forms of DRY violations that should be extracted.
```

### Why It Matters

When duplicated code needs to change, you must find and update every copy. Miss one and you have a bug. Even when the duplication is small, the maintenance risk compounds as the codebase grows. Every duplication is a future inconsistency waiting to happen.

-e 
---

# fn-extract-try-catch

## Extract Try/Catch Bodies Into Their Own Functions

Try/catch blocks are ugly. They mix error processing with normal processing. Extract the bodies into their own functions so each function does one thing.

### Bad

```java
public void delete(Page page) {
    try {
        deletePage(page);
        registry.deleteReference(page.name);
        configKeys.deleteKey(page.name.makeKey());
    } catch (Exception e) {
        logger.log(e.getMessage());
        notifyAdmin(e);
        metrics.incrementFailureCount("page-delete");
    }
}
```

### Clean

```java
public void delete(Page page) {
    try {
        deletePageAndReferences(page);
    } catch (Exception e) {
        logDeleteFailure(e);
    }
}

private void deletePageAndReferences(Page page) {
    deletePage(page);
    registry.deleteReference(page.name);
    configKeys.deleteKey(page.name.makeKey());
}

private void logDeleteFailure(Exception e) {
    logger.log(e.getMessage());
    notifyAdmin(e);
    metrics.incrementFailureCount("page-delete");
}
```

```python
# Bad - mixed concerns in try/catch
def process_order(order):
    try:
        validate(order)
        charge_payment(order)
        send_confirmation(order)
        update_inventory(order)
    except PaymentError as e:
        log.error(f"Payment failed: {e}")
        refund_if_partial(order)
        notify_support(order, e)

# Clean
def process_order(order):
    try:
        fulfill_order(order)
    except PaymentError as e:
        handle_payment_failure(order, e)

def fulfill_order(order):
    validate(order)
    charge_payment(order)
    send_confirmation(order)
    update_inventory(order)

def handle_payment_failure(order, error):
    log.error(f"Payment failed: {error}")
    refund_if_partial(order)
    notify_support(order, error)
```

### Why It Matters

A function that contains a try/catch is doing error handling — that is its one thing. The code within the try and the code within the catch are separate concerns that deserve their own functions. This also makes the error handling logic independently testable.

-e 
---

# fn-no-flag-args

## Never Pass a Boolean to Select Behavior

A boolean argument is a loud declaration that the function does two things — one when true, one when false. Split it into two functions with clear names.

### Bad

```java
public void render(boolean isSuite) {
    if (isSuite) {
        renderForSuite();
    } else {
        renderForSingleTest();
    }
}

// Call site is unreadable
render(true);  // What does true mean here?
```

### Clean

```java
public void renderForSuite() { }
public void renderForSingleTest() { }

// Call site is self-documenting
renderForSuite();
```

```python
# Bad
def create_file(name: str, temp: bool = False):
    if temp:
        ...  # different behavior
    else:
        ...  # different behavior

create_file("report.txt", True)  # True what?

# Clean
def create_file(name: str): ...
def create_temp_file(name: str): ...
```

### Why It Matters

Flag arguments make call sites unreadable. `render(true)` communicates nothing. `renderForSuite()` communicates everything. The boolean also guarantees the function violates the "do one thing" principle.

-e 
---

# fn-no-side-effects

## Functions Should Not Have Hidden Side Effects

A function that promises to do one thing but also quietly does other things is lying. Side effects are lies. They create temporal couplings and hidden dependencies.

### Bad

```java
public boolean checkPassword(String userName, String password) {
    User user = userRepository.findByName(userName);
    if (user != null) {
        String codedPhrase = user.getPhraseEncodedByPassword();
        if (codedPhrase.equals(encrypt(password))) {
            Session.initialize();  // SIDE EFFECT — hidden session reset
            return true;
        }
    }
    return false;
}
```

The name says `checkPassword` but it also initializes the session. A caller who just wants to verify credentials will unknowingly reset an active session.

### Clean

```java
public boolean isValidPassword(String userName, String password) {
    User user = userRepository.findByName(userName);
    if (user == null) return false;
    return user.getPhraseEncodedByPassword().equals(encrypt(password));
}

// Session initialization is explicit and separate
public void authenticateAndStartSession(String userName, String password) {
    if (isValidPassword(userName, password)) {
        session.initialize();
    }
}
```

```python
# Bad - hidden write in a "read" function
def get_config(path: str) -> dict:
    config = load_yaml(path)
    config["last_accessed"] = datetime.now()  # side effect!
    save_yaml(path, config)
    return config

# Clean - reads only read, writes are explicit
def get_config(path: str) -> dict:
    return load_yaml(path)

def touch_config(path: str) -> None:
    config = load_yaml(path)
    config["last_accessed"] = datetime.now()
    save_yaml(path, config)
```

### Why It Matters

Hidden side effects are among the most dangerous bugs because they don't manifest at the call site — they manifest somewhere else, later, in a seemingly unrelated part of the system. Every side effect should be visible from the function name.

-e 
---

# fn-one-level-of-abstraction

## One Level of Abstraction Per Function

Statements within a function should all be at the same level of abstraction. Mixing high-level intent with low-level detail makes functions confusing.

### Bad

```java
public void renderPage() {
    String pageContent = getPageContent();
    // High level ^^
    pageContent = pageContent.replaceAll("&", "&amp;");
    pageContent = pageContent.replaceAll("<", "&lt;");
    // Low level ^^ — string manipulation mixed with page rendering
    appendHeader(pageContent);
    // High level again ^^
}
```

### Clean

```java
public void renderPage() {
    String pageContent = getPageContent();
    String sanitized = escapeHtml(pageContent);
    appendHeader(sanitized);
}

private String escapeHtml(String content) {
    return content
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;");
}
```

```python
# Bad - mixed abstraction levels
def deploy_application(app):
    validate_config(app)                          # high level
    os.makedirs(f"/opt/{app.name}", exist_ok=True)  # low level filesystem
    run_migrations(app)                            # high level
    with open(f"/etc/nginx/{app.name}.conf", "w") as f:  # low level I/O
        f.write(generate_nginx_config(app))
    restart_service(app)                           # high level

# Clean - consistent abstraction
def deploy_application(app):
    validate_config(app)
    create_app_directory(app)
    run_migrations(app)
    configure_reverse_proxy(app)
    restart_service(app)
```

### Why It Matters

Reading code that mixes abstraction levels is like reading a recipe that says "Prepare the sauce. Open the valve on the gas line to allow propane flow. Plate the dish." Your brain has to constantly shift between strategic understanding and mechanical detail. Consistent abstraction lets you read at one speed.

-e 
---

# fn-one-thing

## A Function Should Do One Thing

A function should do one thing, do it well, and do it only. If you can extract another function from it with a name that is not merely a restatement of its implementation, it's doing more than one thing.

### Bad

```java
public void createAndEmailReport(List<Transaction> transactions) {
    // thing 1: filter
    List<Transaction> flagged = new ArrayList<>();
    for (Transaction t : transactions) {
        if (t.getAmount() > 10000) flagged.add(t);
    }
    // thing 2: format
    StringBuilder report = new StringBuilder();
    report.append("Flagged Transactions\n");
    for (Transaction t : flagged) {
        report.append(t.getId()).append(": $").append(t.getAmount()).append("\n");
    }
    // thing 3: send
    emailService.send("compliance@company.com", "Report", report.toString());
}
```

### Clean

```java
public void sendFlaggedTransactionReport(List<Transaction> transactions) {
    List<Transaction> flagged = findFlaggedTransactions(transactions);
    String report = formatReport(flagged);
    emailService.send("compliance@company.com", "Flagged Transactions", report);
}

private List<Transaction> findFlaggedTransactions(List<Transaction> transactions) {
    return transactions.stream()
        .filter(t -> t.getAmount() > REPORTING_THRESHOLD)
        .toList();
}

private String formatReport(List<Transaction> transactions) {
    // single responsibility: formatting
}
```

```python
# The test: can you describe the function without using "and"?
# Bad: "This function filters transactions AND formats a report AND sends an email"
# Clean: "This function orchestrates the flagged transaction report workflow"
```

### Why It Matters

Functions that do one thing are trivially testable, trivially reusable, and trivially replaceable. Functions that do many things are tightly coupled bundles of behavior where a change to any part risks all parts.

-e 
---

# fn-prefer-exceptions

## Use Exceptions Over Return Codes

Returning error codes forces the caller to deal with the error immediately, cluttering the happy path. Exceptions let you separate the error handling from the main logic.

### Bad

```java
public int delete(Page page) {
    if (deletePage(page) == E_OK) {
        if (registry.deleteReference(page.name) == E_OK) {
            if (configKeys.deleteKey(page.name.makeKey()) == E_OK) {
                logger.log("page deleted");
                return E_OK;
            } else {
                logger.log("configKey not deleted");
                return E_ERROR;
            }
        } else {
            logger.log("deleteReference failed");
            return E_ERROR;
        }
    } else {
        logger.log("delete failed");
        return E_ERROR;
    }
}
```

### Clean

```java
public void delete(Page page) {
    deletePage(page);
    registry.deleteReference(page.name);
    configKeys.deleteKey(page.name.makeKey());
    logger.log("page deleted");
}
```

```python
# Bad - return code style
def withdraw(account, amount):
    if account.balance < amount:
        return -1  # What does -1 mean?
    account.balance -= amount
    return 0

# Clean - exception style
def withdraw(account: Account, amount: Money) -> None:
    if account.balance < amount:
        raise InsufficientFundsError(account, amount)
    account.balance -= amount
```

### Why It Matters

Return codes lead to deeply nested structures where the happy path is buried in error-checking boilerplate. Exceptions allow the happy path to read as a clean sequence of steps, with error handling consolidated in a separate block.

-e 
---

# fn-small

## Functions Should Be Small

Functions should be short. 20 lines is a reasonable upper bound. If a function is long, it is almost certainly doing more than one thing. Blocks within `if`, `else`, and `while` statements should be one line long — typically a function call.

### Bad

```java
public void processPayroll(List<Employee> employees) {
    for (Employee e : employees) {
        if (e.isActive()) {
            double basePay = e.getSalary() / 24;
            double tax = 0;
            if (basePay > 5000) {
                tax = basePay * 0.3;
            } else if (basePay > 3000) {
                tax = basePay * 0.2;
            } else {
                tax = basePay * 0.1;
            }
            double deductions = e.getHealthPremium() + e.getRetirementContribution();
            double netPay = basePay - tax - deductions;
            PayStub stub = new PayStub(e.getId(), basePay, tax, deductions, netPay);
            payStubRepository.save(stub);
            if (e.isDirectDeposit()) {
                bankService.transfer(e.getBankAccount(), netPay);
            } else {
                checkService.print(stub);
            }
        }
    }
}
```

### Clean

```java
public void processPayroll(List<Employee> employees) {
    employees.stream()
        .filter(Employee::isActive)
        .forEach(this::payEmployee);
}

private void payEmployee(Employee employee) {
    PayStub stub = calculatePayStub(employee);
    payStubRepository.save(stub);
    disbursePayment(employee, stub);
}

private PayStub calculatePayStub(Employee employee) {
    double basePay = employee.getSemiMonthlyPay();
    double tax = calculateTax(basePay);
    double deductions = calculateDeductions(employee);
    double netPay = basePay - tax - deductions;
    return new PayStub(employee.getId(), basePay, tax, deductions, netPay);
}
```

```python
# Bad - one massive function
def process_payroll(employees):
    for e in employees:
        if e.is_active:
            # ... 40 lines of calculation and side effects ...

# Clean - composed of small focused functions
def process_payroll(employees: list[Employee]) -> None:
    for employee in filter(lambda e: e.is_active, employees):
        pay_employee(employee)

def pay_employee(employee: Employee) -> None:
    stub = calculate_pay_stub(employee)
    pay_stub_repo.save(stub)
    disburse_payment(employee, stub)
```

### Why It Matters

Small functions are easier to read, test, name, and reason about. Every long function hides bugs in its complexity. If you can't see the entire function on screen without scrolling, it's a candidate for extraction.

-e 
---

# err-caller-defined-exceptions

## Define Exception Classes by How the Caller Handles Them

The most important concern when defining exception classes is how they are caught. If multiple exceptions are handled the same way, wrap them in a single exception type.

### Bad

```java
// Caller has to catch each vendor-specific exception separately
try {
    port.open();
} catch (DeviceResponseException e) {
    reportPortError(e);
    logger.log(e.getMessage());
} catch (ATM1212UnlockedException e) {
    reportPortError(e);
    logger.log(e.getMessage());
} catch (GMXError e) {
    reportPortError(e);
    logger.log(e.getMessage());
}
```

### Clean

```java
// Wrap the third-party API and translate to a single exception type
public class LocalPort {
    private ACMEPort innerPort;

    public void open() {
        try {
            innerPort.open();
        } catch (DeviceResponseException | ATM1212UnlockedException | GMXError e) {
            throw new PortDeviceFailure(e);
        }
    }
}

// Caller handles one exception
try {
    port.open();
} catch (PortDeviceFailure e) {
    reportPortError(e);
    logger.log(e.getMessage());
}
```

```python
# Bad - caller catches many specific exceptions identically
try:
    response = http_client.post(url, data)
except ConnectionError:
    return fallback_response()
except TimeoutError:
    return fallback_response()
except SSLError:
    return fallback_response()

# Clean - wrap into a domain-meaningful exception
class ExternalServiceUnavailable(Exception): pass

def call_service(url, data):
    try:
        return http_client.post(url, data)
    except (ConnectionError, TimeoutError, SSLError) as e:
        raise ExternalServiceUnavailable(url) from e

try:
    response = call_service(url, data)
except ExternalServiceUnavailable:
    return fallback_response()
```

### Why It Matters

Exception classes should serve the caller, not mirror the implementation. When the caller handles three exceptions identically, having three distinct types is noise. One wrapper exception with good context is cleaner and more maintainable.

-e 
---

# err-context-in-exceptions

## Provide Context in Exception Messages

Every exception should include enough information to determine the source and location of the error. Include the operation that failed and the relevant inputs.

### Bad

```java
throw new RuntimeException("Failed");
throw new IOException("Error occurred");
```

### Clean

```java
throw new OrderProcessingException(
    String.format("Failed to charge payment for order %s: amount=%s, gateway=%s",
        order.getId(), order.getTotal(), gateway.getName()),
    cause);
```

```python
# Bad
raise ValueError("invalid input")

# Clean
raise ValueError(
    f"Cannot process withdrawal: account={account.id}, "
    f"requested={amount}, available={account.balance}"
)
```

### Why It Matters

When an exception arrives in your logs at 3 AM, the message is all you have. A message that says "Error occurred" tells you nothing. A message that includes the operation, entity, and relevant state lets you diagnose without reproducing.

-e 
---

# err-exceptions-over-codes

## Use Exceptions Instead of Return Codes

Return codes require the caller to check the result immediately, leading to deeply nested structures. Exceptions cleanly separate the happy path from error handling.

See also: `fn-prefer-exceptions` for the function-level perspective.

### Bad

```java
public class ErrorCode {
    public static final int OK = 0;
    public static final int NOT_FOUND = -1;
    public static final int UNAUTHORIZED = -2;
}

int result = repository.save(entity);
if (result == ErrorCode.OK) {
    int notifyResult = notificationService.send(entity);
    if (notifyResult != ErrorCode.OK) {
        // handle notification failure
    }
} else if (result == ErrorCode.NOT_FOUND) {
    // handle not found
}
```

### Clean

```java
try {
    repository.save(entity);
    notificationService.send(entity);
} catch (EntityNotFoundException e) {
    handleNotFound(e);
} catch (NotificationFailedException e) {
    handleNotificationFailure(e);
}
```

```python
# Bad
status = db.save(record)
if status == 0:
    status2 = cache.invalidate(record.key)
    if status2 != 0:
        log.error("cache invalidation failed")
elif status == -1:
    log.error("save failed")

# Clean
try:
    db.save(record)
    cache.invalidate(record.key)
except PersistenceError as e:
    handle_save_failure(e)
except CacheError as e:
    handle_cache_failure(e)
```

### Why It Matters

Error codes create a dependency magnet — every caller must know the complete set of codes. Adding a new error means updating every call site. Exceptions are self-describing and can be handled at the appropriate level of the call stack rather than forcing immediate handling.

-e 
---

# err-no-null-pass

## Don't Pass Null as an Argument

Passing null into a function is worse than returning it. It forces the receiving function to check for null on every parameter, or accept the risk of a NullPointerException deep in its logic.

### Bad

```java
public double calculateMetric(double[] measurements) {
    // What happens when someone calls calculateMetric(null)?
    // NPE on the first line that touches measurements
}

// Defensive but ugly
public double calculateMetric(double[] measurements) {
    if (measurements == null)
        throw new IllegalArgumentException("measurements cannot be null");
    // ...
}
```

### Clean

```java
// Don't pass null in the first place. Use empty arrays, Optional, or overloads.
calculator.calculateMetric(new double[]{});

// If an API truly has optional parameters, make it explicit
public double calculateMetric(double[] measurements, OptionalDouble baseline) { }
```

```python
# Bad
process_order(order, None, None)  # What are those Nones?

# Clean - use default parameters or explicit types
def process_order(
    order: Order,
    discount: Discount | None = None,
    coupon: Coupon | None = None
) -> Receipt:
    ...

# Or better - use a parameter object
@dataclass
class OrderOptions:
    discount: Discount | None = None
    coupon: Coupon | None = None

def process_order(order: Order, options: OrderOptions = OrderOptions()) -> Receipt:
    ...
```

### Why It Matters

Most languages have no good way to deal with a null that was accidentally passed. You either litter every function with defensive null checks (noisy, easy to forget) or accept that a null will eventually cause a crash somewhere far from the actual mistake. The cleanest solution is to make passing null culturally unacceptable in your codebase.

-e 
---

# err-no-null-return

## Don't Return Null

Returning null forces every caller to add a null check. One missed check is a NullPointerException. Use Optional, empty collections, the Special Case pattern, or throw an exception instead.

### Bad

```java
public List<Employee> getEmployees() {
    if (/* no employees */)
        return null;
}

// Every caller must remember to check
List<Employee> employees = getEmployees();
if (employees != null) {
    for (Employee e : employees) { }
}
```

### Clean

```java
// Return empty collection
public List<Employee> getEmployees() {
    if (/* no employees */)
        return Collections.emptyList();
}

// Caller can iterate safely without null checks
for (Employee e : getEmployees()) { }

// Or use Optional for single values
public Optional<Employee> findById(int id) {
    // ...
}

findById(42).ifPresent(this::promote);
```

```python
# Bad
def find_user(user_id: int):
    user = db.query(user_id)
    if user is None:
        return None  # pushes the problem to the caller

# Clean - return a sensible default or raise
def find_user(user_id: int) -> User:
    user = db.query(user_id)
    if user is None:
        raise UserNotFoundError(user_id)
    return user

# Or for collections, return empty
def get_active_users() -> list[User]:
    return db.query_active() or []
```

### Why It Matters

Null is a billion-dollar mistake. Every null return is an implicit contract that says "the caller must check for None before using this." That contract is invisible, unenforced, and inevitably broken. Eliminating null returns eliminates an entire category of runtime errors.

-e 
---

# err-unchecked-exceptions

## Prefer Unchecked Exceptions

Checked exceptions (Java-specific) violate the Open/Closed Principle. A checked exception thrown deep in the call stack forces every method in the chain to declare it, creating a dependency from the lowest level to the highest.

### Bad

```java
// Adding a new checked exception here...
public void readConfig() throws ConfigParseException, FileNotFoundException {
    // ...
}

// ...forces changes all the way up the call chain
public void startApp() throws ConfigParseException, FileNotFoundException {
    readConfig();
}

public void main() throws ConfigParseException, FileNotFoundException {
    startApp();
}
```

### Clean

```java
// Unchecked exceptions propagate without signature changes
public void readConfig() {
    try {
        // ...
    } catch (IOException e) {
        throw new ConfigurationException("Failed to read config", e);
    }
}

// Callers handle if they want, ignore if they don't
public void startApp() {
    readConfig();  // no throws clause needed
}
```

```python
# Python only has unchecked exceptions, so this is the default.
# The principle still applies: don't force callers to handle errors
# they can't meaningfully recover from. Let exceptions propagate
# to the level that can actually do something about them.

def read_config(path: str) -> Config:
    try:
        with open(path) as f:
            return parse_config(f.read())
    except (IOError, ParseError) as e:
        raise ConfigurationError(f"Failed to load {path}") from e
```

### Why It Matters

Checked exceptions break encapsulation. A low-level implementation detail (which specific I/O exception can occur) leaks through every layer of abstraction above it. A change to the lowest module causes cascading signature changes across the entire call stack.

### Java Note

Use `RuntimeException` subclasses for your custom exceptions. Reserve checked exceptions only for cases where the immediate caller truly must handle the condition to continue (extremely rare in practice).

-e 
---

# err-wrap-third-party

## Wrap Third-Party APIs to Normalize Exceptions

When you depend on a third-party library, wrap it behind your own interface. This isolates your code from the library's exception types, API changes, and quirks.

### Bad

```java
// Third-party exceptions leak into your domain
try {
    ACMEClient client = new ACMEClient();
    client.sendRequest(payload);
} catch (ACMEConnectionException e) {
    logger.log(e);
} catch (ACMETimeoutException e) {
    logger.log(e);
} catch (ACMEAuthenticationException e) {
    logger.log(e);
}
```

### Clean

```java
public class PaymentGateway {
    private final ACMEClient client;

    public PaymentResult charge(Money amount, PaymentMethod method) {
        try {
            return translateResponse(client.sendRequest(buildPayload(amount, method)));
        } catch (ACMEConnectionException | ACMETimeoutException e) {
            throw new PaymentUnavailableException("Gateway unreachable", e);
        } catch (ACMEAuthenticationException e) {
            throw new PaymentConfigurationException("Invalid gateway credentials", e);
        }
    }
}

// Your code only knows about your exceptions
try {
    gateway.charge(amount, method);
} catch (PaymentUnavailableException e) {
    scheduleRetry(e);
}
```

```python
class NotificationService:
    def __init__(self, client: TwilioClient):
        self._client = client

    def send_sms(self, to: str, body: str) -> None:
        try:
            self._client.messages.create(to=to, body=body, from_=self._from)
        except TwilioRestException as e:
            raise NotificationFailedError(f"SMS to {to} failed") from e
```

### Why It Matters

Wrapping third-party code minimizes your dependency surface. When you swap vendors or upgrade library versions, only the wrapper changes. Your domain code never sees the third-party types and is completely insulated from their changes. This also makes testing trivial — mock the wrapper, not the vendor SDK.

-e 
---

# test-clean-tests

## Test Code Deserves the Same Quality as Production Code

Test code is not second-class code. If you let test code rot, production code follows. Poorly written tests become a burden that slows development rather than enabling it. Tests are documentation — they specify what the system does.

### Bad

```java
@Test
public void test1() {
    String r = svc.proc("abc", 123, true, null);
    assertTrue(r.contains("OK"));
}
```

### Clean

```java
@Test
public void validOrderReturnsConfirmation() {
    String confirmation = orderService.process(validOrder());
    assertThat(confirmation).contains("CONFIRMED");
}
```

### Why It Matters

Tests that are hard to read are hard to maintain. Tests that are hard to maintain get deleted or ignored. Once the test suite is unreliable, developers stop trusting it, and you lose the entire safety net.

---

# test-readable

## Tests Should Read as Clear Specifications

A well-written test reads like a specification of behavior. Someone unfamiliar with the code should be able to read a test and understand what the system does and what the expected behavior is.

### Bad

```python
def test_thing():
    x = MyClass(1, 2, "a", True)
    x.run()
    assert x.val == 42
```

### Clean

```python
def test_calculator_adds_positive_numbers():
    calculator = Calculator(precision=2)

    result = calculator.add(20, 22)

    assert result == 42
```

### Pattern

Tests read best with clear visual separation between the three phases: setup, action, assertion. Blank lines between phases help.

---

# test-build-operate-check

## Structure Tests as Arrange, Act, Assert

Every test has three phases: build the test data (Arrange), invoke the behavior under test (Act), and check the result (Assert). Keeping these phases visually distinct makes tests scannable.

### Clean

```java
@Test
public void withdrawalReducesBalance() {
    // Arrange
    Account account = new Account(Money.of(1000));

    // Act
    account.withdraw(Money.of(250));

    // Assert
    assertEquals(Money.of(750), account.getBalance());
}
```

```python
def test_withdrawal_reduces_balance():
    # Arrange
    account = Account(balance=Money(1000))

    # Act
    account.withdraw(Money(250))

    # Assert
    assert account.balance == Money(750)
```

### Why It Matters

When the three phases are clearly separated, you can instantly see what's being set up, what action is being tested, and what the expected outcome is. Jumbled tests where these phases are interleaved require careful reading to understand.

---

# test-domain-language

## Build a Domain-Specific Testing Language

Create helper functions and builders that let tests speak in the language of the domain. Tests should read like specifications, not like API call sequences.

### Bad

```java
@Test
public void testDiscount() {
    Map<String, Object> order = new HashMap<>();
    order.put("customerId", 42);
    order.put("items", Arrays.asList(
        Map.of("sku", "ABC", "qty", 3, "price", 10.0),
        Map.of("sku", "DEF", "qty", 1, "price", 50.0)
    ));
    order.put("coupon", "SAVE20");

    Map<String, Object> result = service.calculate(order);

    assertEquals(64.0, ((Number) result.get("total")).doubleValue(), 0.01);
}
```

### Clean

```java
@Test
public void twentyPercentCouponAppliesToOrderTotal() {
    Order order = anOrder()
        .forCustomer(42)
        .withItem("ABC", quantity(3), price(10.00))
        .withItem("DEF", quantity(1), price(50.00))
        .withCoupon("SAVE20")
        .build();

    OrderTotal result = pricingService.calculate(order);

    assertThat(result.total()).isCloseTo(Money.of(64.00), within(0.01));
}
```

```python
def test_twenty_percent_coupon_applies_to_order_total():
    order = (OrderBuilder()
        .for_customer(42)
        .with_item("ABC", qty=3, price=10.00)
        .with_item("DEF", qty=1, price=50.00)
        .with_coupon("SAVE20")
        .build())

    result = pricing_service.calculate(order)

    assert result.total == pytest.approx(Money(64.00))
```

### Why It Matters

Domain-specific test helpers make tests readable by non-developers (product owners, QA). They also reduce duplication across tests and make the test suite resilient to structural changes — when the Order constructor changes, you update one builder, not 200 tests.

-e 
---

# test-fast

## Tests Must Run Fast

Slow tests don't get run. Tests that take seconds instead of milliseconds get skipped during development. When tests don't run, code rots.

### Guidelines

- Unit tests should complete in milliseconds, not seconds
- Mock external dependencies (databases, APIs, filesystem)
- If a test needs a database, it's an integration test — separate it
- Run fast tests on every save, slow tests in CI

```java
// Slow - hits real database
@Test
public void testUserCreation() {
    UserRepository repo = new PostgresUserRepository(dataSource);
    repo.save(new User("matt"));
    assertNotNull(repo.findByName("matt"));
}

// Fast - uses in-memory implementation
@Test
public void testUserCreation() {
    UserRepository repo = new InMemoryUserRepository();
    repo.save(new User("matt"));
    assertNotNull(repo.findByName("matt"));
}
```

```python
# Slow
def test_fetch_user():
    response = requests.get("https://api.example.com/users/1")
    assert response.status_code == 200

# Fast
def test_fetch_user(mocker):
    mocker.patch("requests.get", return_value=MockResponse(200, {"id": 1}))
    user = fetch_user(1)
    assert user.id == 1
```

---

# test-independent

## Tests Should Not Depend on Each Other

No test should set up conditions for the next test. Each test must be runnable in isolation and in any order. Shared mutable state between tests is a recipe for intermittent failures.

### Bad

```java
private static User sharedUser;

@Test public void test1_createUser() {
    sharedUser = service.create("matt");
    assertNotNull(sharedUser);
}

@Test public void test2_deactivateUser() {
    service.deactivate(sharedUser);  // fails if test1 didn't run first
    assertFalse(sharedUser.isActive());
}
```

### Clean

```java
@Test public void createdUserIsActive() {
    User user = service.create("matt");
    assertTrue(user.isActive());
}

@Test public void deactivatedUserIsInactive() {
    User user = service.create("matt");
    service.deactivate(user);
    assertFalse(user.isActive());
}
```

---

# test-repeatable

## Tests Must Produce the Same Result in Any Environment

Tests should pass on your machine, on CI, and on a colleague's laptop. If a test depends on network access, system time, or environment-specific configuration, it's fragile.

### Guidelines

- Don't depend on real external services
- Don't depend on system clock — inject a Clock abstraction
- Don't depend on filesystem paths that vary by OS
- Don't depend on random values without seeding

```python
# Bad - depends on current time
def test_is_expired():
    token = Token(expires_at=datetime(2025, 1, 1))
    assert token.is_expired()  # Passes in 2025, fails in 2024

# Clean - inject time
def test_is_expired():
    token = Token(expires_at=datetime(2025, 1, 1))
    assert token.is_expired(as_of=datetime(2025, 6, 1))
```

---

# test-self-validating

## Tests Should Return Pass or Fail

A test should produce a boolean result — it either passes or fails. Tests that require manual inspection of output, log files, or database state are not tests, they're scripts.

### Bad

```java
@Test
public void testReport() {
    String report = generator.createReport();
    System.out.println(report);  // Developer has to read this and judge
}
```

### Clean

```java
@Test
public void reportIncludesAllDepartments() {
    String report = generator.createReport();
    assertTrue(report.contains("Engineering"));
    assertTrue(report.contains("Marketing"));
}
```

---

# test-timely

## Write Tests Before or Alongside Production Code

Tests written after the fact tend to be afterthoughts. The production code may be hard to test because testability wasn't a design consideration. Writing tests first (or concurrently) keeps the code testable by design.

### Why It Matters

If you write production code first and tests later, you'll find functions that are hard to isolate, dependencies that are hard to mock, and behavior that's hard to verify. The friction of testing reveals design problems — but only if you encounter that friction before the design is locked in.

-e 
---

# test-one-assert-per-concept

## Test One Concept Per Test

Each test should verify a single behavior or concept. This doesn't mean literally one assert statement — it means one logical assertion about one behavior. When a test fails, you should know exactly what broke.

### Bad

```java
@Test
public void testEverything() {
    User user = createUser("matt", "admin");
    assertNotNull(user);
    assertEquals("matt", user.getName());
    assertEquals("admin", user.getRole());
    assertTrue(user.isActive());
    user.deactivate();
    assertFalse(user.isActive());
    assertThrows(IllegalStateException.class, () -> user.performAction());
}
```

### Clean

```java
@Test
public void newUserIsActiveByDefault() {
    User user = createUser("matt", "admin");
    assertTrue(user.isActive());
}

@Test
public void deactivatedUserCannotPerformActions() {
    User user = createUser("matt", "admin");
    user.deactivate();
    assertThrows(IllegalStateException.class, () -> user.performAction());
}
```

```python
# Clean - each test is a specification of one behavior
def test_new_user_is_active_by_default():
    user = create_user("matt", "admin")
    assert user.is_active

def test_deactivated_user_cannot_perform_actions():
    user = create_user("matt", "admin")
    user.deactivate()
    with pytest.raises(InactiveUserError):
        user.perform_action()
```

### Why It Matters

When a multi-concept test fails, you have to read the entire test and the failure message to figure out which concept broke. Single-concept tests fail with a name that tells you exactly what's wrong.

-e 
---

# class-small

## Classes Should Be Small

A class should be small — measured by the number of responsibilities, not lines of code. You should be able to describe what the class does in about 25 words without using "and" or "or."

### Bad

```java
// This class does everything — God class
public class UserManager {
    public User createUser() { }
    public void sendWelcomeEmail() { }
    public void validatePassword() { }
    public Report generateUserReport() { }
    public void syncToLDAP() { }
    public void exportToCSV() { }
}
```

### Clean

```java
public class UserRegistration { }
public class WelcomeNotifier { }
public class PasswordPolicy { }
public class UserReportGenerator { }
```

### Why It Matters

Large classes are hard to understand, hard to test, and hard to change safely. When a class has many responsibilities, changes to one responsibility risk breaking others. Small, focused classes are independently testable and independently deployable.

---

# class-single-responsibility

## Single Responsibility Principle

A class should have one, and only one, reason to change. If you can think of more than one motivation for changing a class, it has more than one responsibility.

### Bad

```java
public class Employee {
    public Money calculatePay() { }       // Accounting rules
    public void save() { }                // Persistence
    public String generateReport() { }    // Reporting format
}
```

Three different stakeholders (accounting, DBA, reporting) would each cause changes to this class.

### Clean

```java
public class Employee { }                        // Domain entity
public class PayCalculator { }                   // Accounting logic
public class EmployeeRepository { }              // Persistence
public class EmployeeReportFormatter { }         // Presentation
```

```python
# Bad - mixed concerns
class Order:
    def calculate_total(self): ...    # business logic
    def save_to_db(self): ...         # persistence
    def send_confirmation(self): ...  # notification

# Clean - separated concerns
class Order:
    def calculate_total(self): ...

class OrderRepository:
    def save(self, order: Order): ...

class OrderNotifier:
    def send_confirmation(self, order: Order): ...
```

### Why It Matters

SRP is the most important class design principle. When each class has one reason to change, modifications are isolated, predictable, and safe. When a class has multiple responsibilities, it becomes a change magnet where every modification carries collateral damage risk.

---

# class-cohesion

## High Cohesion

A class is cohesive when each method uses a high proportion of the class's instance variables. When cohesion drops, it usually means the class should be split.

### Bad

```java
public class UserService {
    private Database db;
    private EmailClient email;
    private PdfGenerator pdf;
    private CacheManager cache;

    // Only uses db
    public User findUser(int id) { return db.query(id); }

    // Only uses email
    public void sendReset(String addr) { email.send(addr, "Reset"); }

    // Only uses pdf
    public byte[] exportReport(User u) { return pdf.generate(u); }
}
```

Three methods, three disjoint sets of dependencies — this is really three classes wearing a trenchcoat.

### Clean

```java
public class UserRepository {
    private Database db;
    public User findById(int id) { }
    public List<User> findByDepartment(String dept) { }
}

public class PasswordResetService {
    private EmailClient email;
    public void sendResetLink(String address) { }
}
```

---

# class-organize-for-change

## Organize Classes to Minimize Change Impact

Structure your classes so that adding new behavior doesn't require modifying existing code. Use interfaces and polymorphism to make the system open for extension but closed for modification.

### Bad

```java
// Adding a new SQL statement type requires modifying this class
public class Sql {
    public String create() { }
    public String insert(Object[] fields) { }
    public String selectAll() { }
    public String findByKey(String key) { }
    // Every new query type = another method here
}
```

### Clean

```java
abstract class Sql {
    abstract String generate();
}

class CreateSql extends Sql { }
class InsertSql extends Sql { }
class SelectSql extends Sql { }
class FindByKeySql extends Sql { }
// New types extend, nothing existing changes
```

---

# class-prefer-polymorphism

## Prefer Polymorphism Over Type Checking

When you see a switch or if/else chain that checks the type of an object to decide what to do, replace it with polymorphism. Each case becomes a subclass with its own implementation.

### Bad

```java
public double calculateArea(Shape shape) {
    switch (shape.type) {
        case CIRCLE:
            return Math.PI * shape.radius * shape.radius;
        case RECTANGLE:
            return shape.width * shape.height;
        case TRIANGLE:
            return 0.5 * shape.base * shape.height;
    }
}
```

### Clean

```java
interface Shape {
    double area();
}

class Circle implements Shape {
    public double area() { return Math.PI * radius * radius; }
}

class Rectangle implements Shape {
    public double area() { return width * height; }
}
```

```python
# Clean - Python uses duck typing naturally
class Circle:
    def area(self) -> float:
        return math.pi * self.radius ** 2

class Rectangle:
    def area(self) -> float:
        return self.width * self.height

# No type checking needed
total_area = sum(shape.area() for shape in shapes)
```

---

# class-law-of-demeter

## Law of Demeter

A method should only call methods on its own object, its parameters, objects it creates, and its direct component objects. Don't chain through objects to reach distant collaborators.

### Bad

```java
// Train wreck — reaching through multiple objects
String city = order.getCustomer().getAddress().getCity();
```

### Clean

```java
// Ask the object to give you what you need
String city = order.getShippingCity();

// Order delegates internally
public String getShippingCity() {
    return customer.getShippingCity();
}
```

```python
# Bad
output_dir = config.get_module("reporting").get_settings().get("output_dir")

# Clean
output_dir = config.get_report_output_dir()
```

### Why It Matters

Method chains create hidden coupling. Your code doesn't just depend on `order` — it depends on the structure of `Customer`, `Address`, and their method signatures. If any link in the chain changes, your code breaks. Delegating through intermediate methods preserves encapsulation.

---

# class-data-transfer-objects

## Use DTOs for Data Transfer

Data Transfer Objects are classes with public fields and no behavior. They're useful for transferring data across boundaries (API responses, database rows, message payloads). Keep them free of business logic.

### Clean

```java
public record EmployeeDTO(
    String name,
    String email,
    String department
) { }
```

```python
@dataclass(frozen=True)
class EmployeeDTO:
    name: str
    email: str
    department: str
```

### Why It Matters

DTOs are the boundary between your domain and the outside world. They should be dumb data carriers. Putting business logic in DTOs couples your domain rules to your transport format, making both harder to change independently.

-e 
---

# comment-why-not-what

## Comments Should Explain Why, Not What

If the code needs a comment to explain what it does, the code should be rewritten to be clearer. Reserve comments for explaining *why* a decision was made — the intent, the tradeoff, the constraint that isn't visible in the code itself.

### Bad

```java
// Check if employee is eligible for benefits
if (employee.getType() == FULL_TIME && employee.getTenure() > 90) {
```

### Clean

```java
if (employee.isEligibleForBenefits()) {
    // 90-day waiting period required by state labor law §4.2.1
```

```python
# Bad - restates the code
# increment counter by one
counter += 1

# Clean - explains a non-obvious decision
# Using insertion sort here because the dataset is nearly sorted
# and insertion sort outperforms quicksort for n < 20
sorted_items = insertion_sort(items)
```

### Why It Matters

"What" comments decay. When the code changes and the comment doesn't, the comment becomes a lie. "Why" comments are durable because the reason behind a decision doesn't change when the implementation does.

---

# comment-no-journal

## Don't Keep Changelogs in Comments

Source control exists. Don't maintain a history of changes in the file header. Git blame tells you who changed what and when, with far more accuracy than a manually maintained comment block.

### Bad

```java
/**
 * Changes:
 * 2024-01-15 - Matt - Added validation for negative amounts
 * 2024-01-10 - Sarah - Fixed NPE in calculateTotal
 * 2023-12-20 - Matt - Initial implementation
 */
```

### Clean

Delete it. Use `git log`, `git blame`, and meaningful commit messages instead.

---

# comment-no-noise

## Remove Comments That Restate the Obvious

Noise comments add clutter without value. Javadoc on every getter, comments that restate the function name, and comments that explain basic language constructs are all noise.

### Bad

```java
/** The name. */
private String name;

/** The version. */
private String version;

/** Default constructor */
public MyClass() { }

/** Returns the name */
public String getName() { return name; }

// Always returns true
private boolean isTrue() { return true; }
```

### Clean

Just delete them. The code already says everything these comments say.

```java
private String name;
private String version;
```

---

# comment-no-closing-brace

## Don't Comment Closing Braces

If a function is so long that you need closing brace comments to keep track of which block you're in, the function is too long. Shorten the function instead.

### Bad

```java
public void processOrders(List<Order> orders) {
    for (Order order : orders) {
        if (order.isActive()) {
            for (LineItem item : order.getItems()) {
                if (item.isInStock()) {
                    // ... 30 lines of logic
                } // if in stock
            } // for items
        } // if active
    } // for orders
} // processOrders
```

### Clean

Extract methods until the nesting is shallow and the function is short enough to see the structure at a glance. No closing brace comments needed.

---

# comment-no-commented-out-code

## Delete Commented-Out Code

Commented-out code sits there and rots. Nobody knows why it was commented out. Nobody knows if it's safe to delete. Everyone is afraid to remove it. Meanwhile it clutters the file and misleads readers.

### Bad

```java
public void calculate() {
    double result = base * rate;
    // result = result * LEGACY_FACTOR;
    // if (useOldFormula) {
    //     result = legacyCalculation(base);
    // }
    return result;
}
```

### Clean

Delete it. If you need it back, it's in version control.

```java
public void calculate() {
    return base * rate;
}
```

---

# comment-legal-headers

## Legal Headers Are Acceptable

Copyright notices and license headers at the top of source files are fine when required by organizational policy or open-source licenses. Keep them short and reference external documents rather than embedding full license text.

### Acceptable

```java
// Copyright 2025 Acme Corp. All rights reserved.
// Licensed under the MIT License. See LICENSE file.
```

---

# comment-todo

## TODO Comments Must Be Tracked

TODO comments are acceptable as temporary markers for work that needs to be done but can't be done right now. They should be specific, include a ticket reference if possible, and be regularly reviewed and resolved.

### Acceptable

```java
// TODO(JIRA-1234): Replace with batch processing when queue service is ready
for (Event event : events) {
    processEvent(event);
}
```

### Not Acceptable

```java
// TODO: fix this later
// TODO: make this better
// TODO: not sure if this works
```

---

# comment-warn-consequences

## Warning Comments Are Valuable

Comments that warn other developers about non-obvious consequences of changing or running code are genuinely helpful. These protect against mistakes that the code itself can't prevent.

### Good

```java
// WARNING: This test takes 30 minutes to run. Only run in nightly CI.
@Test
public void fullIntegrationSuite() { }

// Don't change this format string — the payment gateway rejects
// anything that doesn't match ISO 8601 with timezone offset
private static final String DATE_FORMAT = "yyyy-MM-dd'T'HH:mm:ssXXX";
```

```python
# WARNING: This deletes all records in the staging environment.
# Triple-check you're not pointed at production before running.
def reset_staging():
    ...
```

### Why It Matters

These are "why" comments in disguise. They explain a constraint that isn't visible in the code and prevent future developers from making expensive mistakes.

-e 
---

# format-vertical-density

## Related Code Should Appear Vertically Dense

Lines of code that are tightly related should appear close together. Don't separate them with blank lines or comments. Conversely, use blank lines to separate concepts that are distinct.

### Bad

```java
private String firstName;

// The last name of the user
private String lastName;

// The age of the user
private int age;

public String getFirstName() {

    return firstName;

}
```

### Clean

```java
private String firstName;
private String lastName;
private int age;

public String getFirstName() {
    return firstName;
}
```

```python
# Bad - unrelated blank lines break visual grouping
class User:

    name: str

    email: str

    def activate(self):

        self.active = True

# Clean - density reflects relatedness
class User:
    name: str
    email: str

    def activate(self):
        self.active = True
```

### Why It Matters

Your eye uses vertical whitespace to infer structure. When related things are spread apart, the reader assumes they're unrelated. When unrelated things are squeezed together, the reader assumes they're connected. Vertical density should match conceptual affinity.

---

# format-vertical-distance

## Variables Declared Close to Their Usage

Local variables should be declared as close to their first usage as possible. Don't declare all variables at the top of a function — that forces the reader to hold them in memory across unrelated code.

### Bad

```java
public void processReport() {
    String title;
    String author;
    List<Section> sections;
    DateFormat formatter;
    int pageCount;

    // ... 30 lines later, title is first used
    title = report.getTitle();
}
```

### Clean

```java
public void processReport() {
    String title = report.getTitle();
    renderHeader(title);

    List<Section> sections = report.getSections();
    for (Section section : sections) {
        renderSection(section);
    }
}
```

```python
# Bad - declared far from usage
def generate_invoice(order):
    tax_rate = 0.08
    discount = 0.0
    shipping = 0.0
    # ... 20 lines of unrelated logic ...
    total = subtotal * (1 + tax_rate)

# Clean - declared at point of use
def generate_invoice(order):
    subtotal = calculate_subtotal(order)
    tax_rate = get_tax_rate(order.shipping_state)
    total = subtotal * (1 + tax_rate)
```

### Instance Variables

Instance variables are the exception — they should be declared at the top of the class in a well-known location so everyone knows where to look. The Java convention is top of class. Python uses `__init__`.

---

# format-dependent-functions

## Caller Above Callee

If one function calls another, the caller should be above the callee in the source file. This creates a natural top-down reading flow where you encounter the high-level orchestration first and can drill into details as needed.

### Clean

```java
public void buildReport() {          // reads first
    Header header = buildHeader();
    Body body = buildBody();
    Footer footer = buildFooter();
    assemble(header, body, footer);
}

private Header buildHeader() { }     // reads second if curious
private Body buildBody() { }
private Footer buildFooter() { }
private void assemble(...) { }
```

```python
# Top-level function at the top
def deploy(app: Application) -> None:
    validate(app)
    build_artifact(app)
    push_to_registry(app)
    update_service(app)

# Supporting functions below
def validate(app: Application) -> None: ...
def build_artifact(app: Application) -> None: ...
def push_to_registry(app: Application) -> None: ...
def update_service(app: Application) -> None: ...
```

### Why It Matters

Code reads like a newspaper — headlines and lead paragraphs at the top, details further down. The reader should be able to understand the high-level flow without scrolling past implementation details they don't care about yet.

---

# format-newspaper-rule

## Files Read Top-to-Bottom Like a Newspaper

The name of the file should be enough to tell you whether you're in the right module. The topmost functions should give you the high-level concepts. Detail should increase as you move down the file.

### Structure

1. **Module/class docstring** (if needed) — the headline
2. **Constants and configuration**
3. **Public API** — the high-level story
4. **Private helpers** — the supporting details

```python
"""Order processing module."""

MAX_RETRY_ATTEMPTS = 3
TIMEOUT_SECONDS = 30

# Public API
def process_order(order: Order) -> Receipt:
    validated = validate(order)
    charged = charge_payment(validated)
    return generate_receipt(charged)

# Private helpers
def validate(order: Order) -> ValidatedOrder: ...
def charge_payment(order: ValidatedOrder) -> ChargedOrder: ...
def generate_receipt(order: ChargedOrder) -> Receipt: ...
```

---

# format-team-rules

## The Team Agrees on One Set of Rules

Individual style preferences don't matter. What matters is that the entire team uses the same formatting conventions. Consistency trumps personal preference.

### Guidelines

- Use a shared formatter config (Prettier, Black, google-java-format, Spotless)
- Commit the config file to the repo
- Run the formatter in CI so violations fail the build
- Stop debating tabs vs spaces — let the tool decide and move on

```python
# pyproject.toml — checked into repo, enforced in CI
[tool.black]
line-length = 100
target-version = ["py312"]

[tool.isort]
profile = "black"
```

```xml
<!-- Java — Spotless in build.gradle or pom.xml -->
<plugin>
    <groupId>com.diffplug.spotless</groupId>
    <artifactId>spotless-maven-plugin</artifactId>
    <configuration>
        <java>
            <googleJavaFormat/>
        </java>
    </configuration>
</plugin>
```

### Why It Matters

Inconsistent formatting creates noisy diffs, merge conflicts over whitespace, and cognitive load when reading code that looks different in every file. A shared formatter eliminates all of this for zero ongoing effort.

---

# format-horizontal-limit

## Lines Should Be Short Enough to Read Without Scrolling

Long lines force horizontal scrolling or wrapping, both of which break reading flow. A reasonable limit is 100-120 characters. If a line exceeds that, it usually means the expression is too complex or the nesting is too deep.

### Bad

```java
public List<EmployeeDTO> getActiveEmployeesWithBenefitsInDepartment(String departmentName, boolean includeContractors, Date startDate, Date endDate) {
```

### Clean

```java
public List<EmployeeDTO> findEligibleEmployees(
        EligibilityQuery query) {
```

```python
# Bad
result = some_service.do_something(first_very_long_argument, second_very_long_argument, third_very_long_argument, fourth_very_long_argument)

# Clean
result = some_service.do_something(
    first_very_long_argument,
    second_very_long_argument,
    third_very_long_argument,
    fourth_very_long_argument,
)
```

### Why It Matters

Most developers work with side-by-side windows, diff views, or code review tools that have limited horizontal space. Lines that fit within a reasonable width are readable in all of these contexts without mental gymnastics.

-e 
---

# boundary-wrap-third-party

## Wrap Third-Party APIs Behind Your Own Interfaces

Third-party code changes on someone else's schedule. Wrapping it behind your own interface insulates your codebase from their breaking changes, normalizes their API to your conventions, and makes testing trivial.

### Bad

```java
// Third-party API scattered throughout your code
Map<String, Sensor> sensors = new HashMap<>();
Sensor s = new Sensor();
sensors.put("living-room", s);

// Later, somewhere else entirely
Sensor s = sensors.get("kitchen");
s.getTemperature();
```

### Clean

```java
public class SensorRegistry {
    private final Map<String, Sensor> sensors = new HashMap<>();

    public void register(String location, Sensor sensor) {
        sensors.put(location, sensor);
    }

    public Sensor getByLocation(String location) {
        Sensor sensor = sensors.get(location);
        if (sensor == null) {
            throw new SensorNotFoundException(location);
        }
        return sensor;
    }

    public double getTemperature(String location) {
        return getByLocation(location).getTemperature();
    }
}
```

```python
# Bad - boto3 calls scattered everywhere
s3 = boto3.client("s3")
s3.upload_fileobj(file, "my-bucket", key)

# Clean - wrapped behind your interface
class FileStorage:
    def __init__(self, bucket: str):
        self._client = boto3.client("s3")
        self._bucket = bucket

    def upload(self, key: str, content: BinaryIO) -> str:
        try:
            self._client.upload_fileobj(content, self._bucket, key)
            return f"s3://{self._bucket}/{key}"
        except ClientError as e:
            raise StorageError(f"Upload failed: {key}") from e

    def download(self, key: str) -> bytes:
        try:
            buf = io.BytesIO()
            self._client.download_fileobj(self._bucket, key, buf)
            return buf.getvalue()
        except ClientError as e:
            raise StorageError(f"Download failed: {key}") from e
```

### Why It Matters

When you wrap third-party code, you control the surface area of your dependency. Swapping S3 for GCS or Twilio for SendGrid becomes a change to one wrapper class, not a codebase-wide find-and-replace. Your tests mock the wrapper interface, never the vendor SDK.

---

# boundary-learning-tests

## Write Learning Tests Against Third-Party Code

When adopting a new library, write small focused tests that exercise the features you plan to use. These tests serve two purposes: they teach you the API, and they detect when an upgrade changes behavior you depend on.

### Clean

```java
@Test
public void jacksonDeserializesNestedObjects() {
    String json = """
        {"name": "matt", "address": {"city": "Apex"}}
        """;
    ObjectMapper mapper = new ObjectMapper();
    User user = mapper.readValue(json, User.class);

    assertEquals("matt", user.getName());
    assertEquals("Apex", user.getAddress().getCity());
}

@Test
public void jacksonHandlesMissingFieldsWithDefaults() {
    String json = """
        {"name": "matt"}
        """;
    ObjectMapper mapper = new ObjectMapper();
    mapper.configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
    User user = mapper.readValue(json, User.class);

    assertEquals("matt", user.getName());
    assertNull(user.getAddress());
}
```

```python
# Learning tests for httpx
def test_httpx_follows_redirects():
    """Verify httpx follows redirects by default."""
    response = httpx.get("http://httpbin.org/redirect/1")
    assert response.status_code == 200

def test_httpx_timeout_raises():
    """Verify httpx raises on timeout."""
    with pytest.raises(httpx.TimeoutException):
        httpx.get("http://httpbin.org/delay/5", timeout=1)
```

### Why It Matters

Without learning tests, you discover library behavior changes when production breaks. With them, your CI pipeline catches the change the moment you upgrade the dependency. They're cheap to write and expensive to not have.

---

# boundary-adapter-pattern

## Use Adapters for Code That Doesn't Exist Yet

When you need to integrate with a system or API that hasn't been built yet, define the interface you wish you had. Build an adapter that satisfies that interface. When the real thing arrives, you write one adapter — the rest of your code doesn't change.

### Clean

```java
// Define the interface you want
public interface TranscriptionService {
    Transcript transcribe(AudioFile audio);
}

// Build against the interface now
public class MeetingProcessor {
    private final TranscriptionService transcription;

    public MeetingSummary process(AudioFile recording) {
        Transcript transcript = transcription.transcribe(recording);
        return summarize(transcript);
    }
}

// Fake implementation for development
public class StubTranscriptionService implements TranscriptionService {
    public Transcript transcribe(AudioFile audio) {
        return Transcript.of("Fake transcript for testing");
    }
}

// Real adapter when the service is ready
public class WhisperTranscriptionService implements TranscriptionService {
    public Transcript transcribe(AudioFile audio) {
        // calls Whisper API
    }
}
```

```python
# Define what you need
class PaymentProcessor(Protocol):
    def charge(self, amount: Money, method: PaymentMethod) -> Receipt: ...

# Build against the protocol
class OrderService:
    def __init__(self, payments: PaymentProcessor):
        self._payments = payments

    def checkout(self, order: Order) -> Receipt:
        return self._payments.charge(order.total, order.payment_method)

# Stub for now
class FakePayments:
    def charge(self, amount: Money, method: PaymentMethod) -> Receipt:
        return Receipt(status="approved", amount=amount)

# Swap in the real one later without touching OrderService
class StripePayments:
    def charge(self, amount: Money, method: PaymentMethod) -> Receipt:
        # calls Stripe API
        ...
```

### Why It Matters

The adapter pattern at boundaries keeps your code decoupled from external systems. You can develop, test, and demo your application before the external dependency exists. When it arrives, integration is a single class rather than a refactor.

