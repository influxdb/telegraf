# JSON Parser Version 2 Plugin

This parser takes valid JSON input and turns it into line protocol. The query
syntax supported is [GJSON Path
Syntax](https://github.com/tidwall/gjson/blob/v1.7.5/SYNTAX.md), you can go to
this playground to test out your GJSON path here:
[gjson.dev/](https://gjson.dev). You can find multiple examples under the
[`testdata`][] folder.

> [!WARNING]
> In the current state of the implementation, the json_v2 parser should be avoided in favor of the [XPath Parser](../xpath), especially when working with arrays.

## Configuration

```toml
 [[inputs.file]]
    urls = []
    data_format = "json_v2"
    [[inputs.file.json_v2]]
        measurement_name = "" # A string that will become the new measurement name
        measurement_name_path = "" # A string with valid GJSON path syntax, will override measurement_name
        timestamp_path = "" # A string with valid GJSON path syntax to a valid timestamp (single value)
        timestamp_format = "" # A string with a valid timestamp format (see below for possible values)
        timestamp_timezone = "" # A string with with a valid timezone (see below for possible values)
        [[inputs.file.json_v2.tag]]
            path = "" # A string with valid GJSON path syntax to a non-array/non-object value
            rename = "new name" # A string with a new name for the tag key
            ## Setting optional to true will suppress errors if the configured Path doesn't match the JSON
            optional = false
        [[inputs.file.json_v2.field]]
            path = "" # A string with valid GJSON path syntax to a non-array/non-object value
            rename = "new name" # A string with a new name for the tag key
            type = "int" # A string specifying the type (int,uint,float,string,bool)
            ## Setting optional to true will suppress errors if the configured Path doesn't match the JSON
            optional = false
        [[inputs.file.json_v2.object]]
            path = "" # A string with valid GJSON path syntax, can include array's and object's

            ## Setting optional to true will suppress errors if the configured Path doesn't match the JSON
            optional = false

            ## Configuration to define what JSON keys should be used as timestamps ##
            timestamp_key = "" # A JSON key (for a nested key, prepend the parent keys with underscores) to a valid timestamp
            timestamp_format = "" # A string with a valid timestamp format (see below for possible values)
            timestamp_timezone = "" # A string with with a valid timezone (see below for possible values)

            ### Configuration to define what JSON keys should be included and how (field/tag) ###
            tags = [] # List of JSON keys (for a nested key, prepend the parent keys with underscores) to be a tag instead of a field, when adding a JSON key in this list you don't have to define it in the included_keys list
            included_keys = [] # List of JSON keys (for a nested key, prepend the parent keys with underscores) that should be only included in result
            excluded_keys = [] # List of JSON keys (for a nested key, prepend the parent keys with underscores) that shouldn't be included in result
            # When a tag/field sub-table is defined, they will be the only field/tag's along with any keys defined in the included_keys list.
            # If the resulting values aren't included in the object/array returned by the root object path, it won't be included.
            # You can define as many tag/field sub-tables as you want.
            [[inputs.file.json_v2.object.tag]]
                path = "" # # A string with valid GJSON path syntax, can include array's and object's
                rename = "new name" # A string with a new name for the tag key
            [[inputs.file.json_v2.object.field]]
                path = "" # # A string with valid GJSON path syntax, can include array's and object's
                rename = "new name" # A string with a new name for the tag key
                type = "int" # A string specifying the type (int,uint,float,string,bool)

            ### Configuration to modify the resulting line protocol ###
            disable_prepend_keys = false (or true, just not both)
            [inputs.file.json_v2.object.renames] # A map of JSON keys (for a nested key, prepend the parent keys with underscores) with a new name for the tag key
                key = "new name"
            [inputs.file.json_v2.object.fields] # A map of JSON keys (for a nested key, prepend the parent keys with underscores) with a type (int,uint,float,string,bool)
                key = "int"
```

You configure this parser by describing the line protocol you want by defining
the fields and tags from the input. The configuration is divided into config
sub-tables called `field`, `tag`, and `object`. In the example below you can see
all the possible configuration keys you can define for each config table. In the
sections that follow these configuration keys are defined in more detail.

---

### root config options

* **measurement_name (OPTIONAL)**:  Will set the measurement name to the provided string.
* **measurement_name_path (OPTIONAL)**: You can define a query with [GJSON Path Syntax](https://github.com/tidwall/gjson/blob/v1.7.5/SYNTAX.md) to set a measurement name from the JSON input. The query must return a single data value or it will use the default measurement name. This takes precedence over `measurement_name`.
* **timestamp_path (OPTIONAL)**: You can define a query with [GJSON Path Syntax](https://github.com/tidwall/gjson/blob/v1.7.5/SYNTAX.md) to set a timestamp from the JSON input. The query must return a single data value or it will default to the current time.
* **timestamp_format (OPTIONAL, but REQUIRED when timestamp_path is defined**: Must be set to `unix`, `unix_ms`, `unix_us`, `unix_ns`, or
the Go "reference time" which is defined to be the specific time:
`Mon Jan 2 15:04:05 MST 2006`
* **timestamp_timezone (OPTIONAL, but REQUIRES timestamp_path**: This option should be set to a
[Unix TZ value](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones),
such as `America/New_York`, to `Local` to utilize the system timezone, or to `UTC`. Defaults to `UTC`

---

### `field` and `tag` config options

`field` and `tag` represent the elements of [line protocol][lp-ref]. You can use
the `field` and `tag` config tables to gather a single value or an array of
values that all share the same type and name. With this you can add a field or
tag to a line protocol from data stored anywhere in your JSON. If you define the
GJSON path to return a single value then you will get a single resulting line
protocol that contains the field/tag. If you define the GJSON path to return an
array of values, then each field/tag will be put into a separate line protocol
(you use the # character to retrieve JSON arrays, find examples
[here](https://github.com/tidwall/gjson/blob/v1.7.5/SYNTAX.md#arrays)).

Note that objects are handled separately, therefore if you provide a path that
returns a object it will be ignored. You will need use the `object` config table
to parse objects, because `field` and `tag` doesn't handle relationships between
data. Each `field` and `tag` you define is handled as a separate data point.

The notable difference between `field` and `tag`, is that `tag` values will
always be type string while `field` can be multiple types. You can define the
type of `field` to be any [type that line protocol supports][types], which are:

* float
* int
* uint
* string
* bool

[lp-ref]: https://docs.influxdata.com/influxdb/v2.0/reference/syntax/line-protocol/

[types]: https://docs.influxdata.com/influxdb/v2.0/reference/syntax/line-protocol/#data-types-and-format

#### **field**

Using this field configuration you can gather a non-array/non-object
values. Note this acts as a global field when used with the `object`
configuration, if you gather an array of values using `object` then the field
gathered will be added to each resulting line protocol without acknowledging its
location in the original JSON. This is defined in TOML as an array table using
double brackets.

* **path (REQUIRED)**: A string with valid GJSON path syntax to a non-array/non-object value
* **name (OPTIONAL)**: You can define a string value to set the field name. If not defined it will use the trailing word from the provided query.
* **type (OPTIONAL)**: You can define a string value to set the desired type (float, int, uint, string, bool). If not defined it won't enforce a type and default to using the original type defined in the JSON (bool, float, or string).
* **optional (OPTIONAL)**: Setting optional to true will suppress errors if the configured Path doesn't match the JSON. This should be used with caution because it removes the safety net of verifying the provided path. An example case to use this is with the `inputs.mqtt_consumer` plugin when you are expecting multiple JSON files.

#### **tag**

Using this tag configuration you can gather a non-array/non-object values. Note
this acts as a global tag when used with the `object` configuration, if you
gather an array of values using `object` then the tag gathered will be added to
each resulting line protocol without acknowledging its location in the original
JSON. This is defined in TOML as an array table using double brackets.

* **path (REQUIRED)**: A string with valid GJSON path syntax to a non-array/non-object value
* **name (OPTIONAL)**: You can define a string value to set the field name. If not defined it will use the trailing word from the provided query.
* **optional (OPTIONAL)**: Setting optional to true will suppress errors if the configured Path doesn't match the JSON. This should be used with caution because it removes the safety net of verifying the provided path. An example case to use this is with the `inputs.mqtt_consumer` plugin when you are expecting multiple JSON files.

For good examples in using `field` and `tag` you can reference the following
example configs:

---

### object

With the configuration section `object`, you can gather values from [JSON
objects](https://www.w3schools.com/js/js_json_objects.asp). This is defined in
TOML as an array table using double brackets.

#### The following keys can be set for `object`

* **path (REQUIRED)**: You must define the path query that gathers the object with [GJSON Path Syntax](https://github.com/tidwall/gjson/blob/v1.7.5/SYNTAX.md)
* **optional (OPTIONAL)**: Setting optional to true will suppress errors if the configured Path doesn't match the JSON. This should be used with caution because it removes the safety net of verifying the provided path. An example case to use this is with the `inputs.mqtt_consumer` plugin when you are expecting multiple JSON files.

*Keys to define what JSON keys should be used as timestamps:*

* **timestamp_key(OPTIONAL)**: You can define a json key (for a nested key, prepend the parent keys with underscores) for the value to be set as the timestamp from the JSON input.
* **timestamp_format (OPTIONAL, but REQUIRED when timestamp_key is defined**: Must be set to `unix`, `unix_ms`, `unix_us`, `unix_ns`, or
the Go "reference time" which is defined to be the specific time:
`Mon Jan 2 15:04:05 MST 2006`
* **timestamp_timezone (OPTIONAL, but REQUIRES timestamp_key**: This option should be set to a
[Unix TZ value](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones),
such as `America/New_York`, to `Local` to utilize the system timezone, or to `UTC`. Defaults to `UTC`

*Configuration to define what JSON keys should be included and how (field/tag):*

* **included_keys (OPTIONAL)**: You can define a list of key's that should be the only data included in the line protocol, by default it will include everything.
* **excluded_keys (OPTIONAL)**: You can define json keys to be excluded in the line protocol, for a nested key, prepend the parent keys with underscores
* **tags (OPTIONAL)**: You can define json keys to be set as tags instead of fields, if you define a key that is an array or object then all nested values will become a tag
* **field (OPTIONAL, defined in TOML as an array table using double brackets)**: Identical to the [field](#field) table you can define, but with two key differences. The path supports arrays and objects and is defined under the object table and therefore will adhere to how the JSON is structured. You want to use this if you want the field/tag to be added as it would if it were in the included_key list, but then use the GJSON path syntax.
* **tag (OPTIONAL, defined in TOML as an array table using double brackets)**: Identical to the [tag](#tag) table you can define, but with two key differences. The path supports arrays and objects and is defined under the object table and therefore will adhere to how the JSON is structured. You want to use this if you want the field/tag to be added as it would if it were in the included_key list, but then use the GJSON path syntax.

*Configuration to modify the resulting line protocol:*

* **disable_prepend_keys (OPTIONAL)**: Set to true to prevent resulting nested data to contain the parent key prepended to its key **NOTE**: duplicate names can overwrite each other when this is enabled
* **renames (OPTIONAL, defined in TOML as a table using single bracket)**: A table matching the json key with the desired name (opposed to defaulting to using the key), use names that include the prepended keys of its parent keys for nested results
* **fields (OPTIONAL, defined in TOML as a table using single bracket)**: A table matching the json key with the desired type (int,string,bool,float), if you define a key that is an array or object then all nested values will become that type

## Arrays and Objects

The following describes the high-level approach when parsing arrays and objects:

**Array**: Every element in an array is treated as a *separate* line protocol

**Object**: Every key/value in a object is treated as a *single* line protocol

When handling nested arrays and objects, these above rules continue to apply as
the parser creates line protocol. When an object has multiple array's as values,
the array's will become separate line protocol containing only non-array values
from the object. Below you can see an example of this behavior, with an input
json containing an array of book objects that has a nested array of characters.

Example JSON:

```json
{
    "book": {
        "title": "The Lord Of The Rings",
        "chapters": [
            "A Long-expected Party",
            "The Shadow of the Past"
        ],
        "author": "Tolkien",
        "characters": [
            {
                "name": "Bilbo",
                "species": "hobbit"
            },
            {
                "name": "Frodo",
                "species": "hobbit"
            }
        ],
        "random": [
            1,
            2
        ]
    }
}

```

Example configuration:

```toml
[[inputs.file]]
    files = ["./testdata/multiple_arrays_in_object/input.json"]
    data_format = "json_v2"
    [[inputs.file.json_v2]]
        [[inputs.file.json_v2.object]]
            path = "book"
            tags = ["title"]
            disable_prepend_keys = true
```

Expected line protocol:

```text
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",chapters="A Long-expected Party"
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",chapters="The Shadow of the Past"
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",name="Bilbo",species="hobbit"
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",name="Frodo",species="hobbit"
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",random=1
file,title=The\ Lord\ Of\ The\ Rings author="Tolkien",random=2

```

You can find more complicated examples under the folder [`testdata`][].

[`testdata`]: https://github.com/influxdata/telegraf/tree/master/plugins/parsers/json_v2/testdata

## Types

For each field you have the option to define the types. The following rules are
in place for this configuration:

* If a type is explicitly defined, the parser will enforce this type and convert the data to the defined type if possible. If the type can't be converted then the parser will fail.
* If a type isn't defined, the parser will use the default type defined in the JSON (int, float, string)

The type values you can set:

* `int`, bool, floats or strings (with valid numbers) can be converted to a int.
* `uint`, bool, floats or strings (with valid numbers) can be converted to a uint.
* `string`, any data can be formatted as a string.
* `float`, string values (with valid numbers) or integers can be converted to a float.
* `bool`, the string values "true" or "false" (regardless of capitalization) or the integer values `0` or `1`  can be turned to a bool.
