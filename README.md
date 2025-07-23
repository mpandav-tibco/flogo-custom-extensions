# Flogo Custom Extensions

This repository hosts a collection of custom activities and triggers developed for TIBCO Flogo, extending its capabilities to meet specific integration needs.

## Table of Contents

* [Overview](#overview)
* [Custom Activities](#custom-activities)
    * [Dynamic Log Activity](#dynamic-log-activity)
    * [JSON Schema to XSD Activity](#json-schema-to-xsd-activity)
    * [XSD to JSON Schema Activity](#xsd-to-json-schema-activity)
    * [XML Filter Activity](#xml-filter-activity)
* [Custom Triggers](#custom-triggers)
    * [PostgreSQL Lister Trigger](#postgresql-lister-trigger)
* [Examples](#examples)
* [Usage](#usage)
* [Contributing](#contributing)
* [License](#license)

## Overview

TIBCO Flogo is an ultra-lightweight integration framework ideal for edge computing and serverless applications. While Flogo provides a rich set of built-in activities and triggers, this repository contains custom-built extensions designed to address unique integration patterns and functionalities required by our projects.

These extensions are intended to be easily integrated into your Flogo applications, allowing you to leverage their features within your flows.

## Custom Activities

The `activity` folder contains the following custom Flogo activities:

### Dynamic Log Activity

This activity provides enhanced logging capabilities, allowing for more flexible and dynamic control over logging messages within a Flogo flow. It can be particularly useful for debugging or for injecting variable data into logs at runtime.

### JSON Schema to XSD Activity

Transforms a given JSON Schema into an equivalent XML Schema Definition (XSD). This is highly useful in scenarios where you need to integrate JSON-based systems with XML-based systems, enabling schema validation and transformation across different data formats.

### XSD to JSON Schema Activity

Converts an XML Schema Definition (XSD) into its corresponding JSON Schema. Similar to its counterpart, this activity facilitates interoperability between XML and JSON data models, allowing you to generate JSON schemas from existing XML definitions.

### XML Filter Activity

Provides functionality to filter XML content based on specified criteria. This activity can be used to extract, modify, or remove specific elements or attributes from an XML document, offering fine-grained control over XML data processing within Flogo flows.

## Custom Triggers

The `trigger` folder currently holds the following custom Flogo trigger:

### PostgreSQL Lister Trigger

This trigger is designed to listen for and process events or changes from a PostgreSQL database. It can be configured to poll tables, listen to notification events, or react to other database-level changes, making it ideal for event-driven architectures built around PostgreSQL.

## Examples

The `examples` folder provides practical implementations demonstrating the usage of some of our custom extensions:

* **XSD to JSON Schema Usage:** An example Flogo application showcasing how to use the `XSD to JSON Schema Activity` to perform conversions.
* **JSON Schema to XSD Usage:** An example Flogo application demonstrating the application of the `JSON Schema to XSD Activity` for transforming schemas.

These examples serve as a guide to help you quickly understand and implement the custom activities in your own Flogo projects.

## Usage

To use these custom extensions in your Flogo application:

1.  **Clone this repository:**
    ```bash
    git clone https://github.com/mpandav-tibco/flogo-custom-extensions.git
    ```
2.  **Install the extensions:**
    * Navigate into the `activity` or `trigger` directory of the specific extension you want to use.
    * Copy the extension source code to specific directory on your installation.
    * Provide that director url within Flogo VSCode extension -> Settings -> Flogo › Extensions: Local [Steps are documented in official docs](https://docs.tibco.com/pub/flogo-vscode/1.2.0/doc/html/Default.htm#flogo-vscode-user-guide/using-extensions/Uploading_Extensions.htm?).
    * <img width="2096" height="862" alt="image" src="https://github.com/user-attachments/assets/bd240c17-b66b-48b1-94a7-c375dac9b7bc" />

3.  **Build your Flogo application:**
    * After installing the extensions, you can build your Flogo application. The custom extensions will be bundled with your application.

For detailed instructions on building and running Flogo applications, refer to the [official TIBCO Flogo documentation](https://docs.tibco.com/pub/flogo-vscode/1.2.0/doc/html/Default.htm).

## Contributing

We welcome contributions to this repository! If you have an idea for a new extension, find a bug, or want to improve an existing one, please feel free to:

1.  Fork the repository.
2.  Create a new branch (`git checkout -b feature/your-feature-name`).
3.  Make your changes.
4.  Commit your changes (`git commit -am 'Add new feature'`).
5.  Push to the branch (`git push origin feature/your-feature-name`).
6.  Create a new Pull Request.

Please ensure your code adheres to Flogo's extension development guidelines and includes appropriate tests.

## License

This project is licensed under the [MIT License](LICENSE) - see the `LICENSE` file for details.






