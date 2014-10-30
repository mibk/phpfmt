<?php

// 1. normalizovat if/elseif/else/try/catch/while/for/foreach
// 2. odstranit prázdné řádky na 1 prázdný řádek
// 3. true, false, null -- velké
// 4. odstranit trailing whitespace
// 5. trailing newline on EOF

require __DIR__ . '/../vendor/autoload.php';
set_time_limit(0);
date_default_timezone_set('Europe/Prague');

$options = (new PhpOptions\Options())
	->setOption('help')
	->setOption('version', NULL)
	->setOption('write')
	;

$args = $_SERVER['argv'];
array_shift($args);
try {
	$options->parse($args, FALSE);
} catch (Exception $e) {
	if ($e instanceof PhpOptions\UnknownOptionException) {
		echo 'Unknown option -'.($e->isLongOption() ? '-' : '').$e->getName().".\n";

	} elseif ($e instanceof PhpOptions\MissingArgumentException) {
		echo 'Missing argument for option -'.($e->isLongOption()
			? '-'.$e->getOption()->getLongName()
			: $e->getOption()->getShortName()).".\n";

	} elseif ($e instanceof PhpOptions\UnexpectedArgumentException) {
		echo 'Unexpected argument for option -'.($e->isLongOption()
			? '-'.$e->getOption()->getLongName()
			: $e->getOption()->getShortName()).".\n";
	} else {
		throw $e;
	}
	exit(1);
}
$args = $options->getArguments();

function usage($code = 0) {
	echo "Usage: phpfmt [-w] <filename>\n";
	exit($code);
}

$write = FALSE;
foreach ($options as $opt => $value) {
	switch ($opt) {
		case 'help':
			usage(1);
		case 'version':
			echo "phpfmt version @dev\n";
			exit;
		case 'write':
			$write = TRUE;
	}
}

$args || usage(1);

list($file) = $args;

$content = file_get_contents($file === '-' ? 'php://stdin' : $file);

$content = fmt($content);
$content = orderUseStatements($content);
$content = alignColumns($content);
$content = convertSpacesToTabs($content);
$content = removeTrailingWhitespace($content);
$content = ensureTrailingEol($content);

if ($write && $file !== '-') {
	file_put_contents($file, $content);
} else {
	file_put_contents('php://stdout', $content);
}

// Functions

function fmt($content) {
	$doWhile = FALSE;
	$expectIf = FALSE;
	$braceOnNextLine = FALSE;
	$searchingFunction = FALSE;
	$writeSpace = FALSE;
	$catchParenthesis = FALSE;
	$expectBrace = FALSE;
	$braceAfterSemicolon = FALSE;
	$output = new Output;
	$indent = NULL;

	$tokens = token_get_all($content);
	for ($i = 0; $i < count($tokens); $i++) {
		$t = $tokens[$i];
		list($name, $value) = sanitizeToken($t);

		if ($writeSpace) {
			$writeSpace = FALSE;
			if ($name === T_WHITESPACE) {
				$value = ' ';
			} else {
				$output->push(T_WHITESPACE, ' ');
			}

		}
		if ($catchParenthesis === 0) {
			$catchParenthesis = FALSE;
			$expectBrace = TRUE;
		}

		if ($name === T_WHITESPACE) {
			if (isOneLineComment($output->getName(), $output->getValue())) {
				$output->setValue(rtrim($output->getValue()));
				$value = PHP_EOL.$value;
			}
			$value = removeEmptyLines($value);

			// Function parameter on multiple lines
			if (strpos($value, "\n") !== FALSE && $output->getValue() === ',') {
				$braceOnNextLine = FALSE;
			}

		} elseif ($expectBrace) {
			$expectBrace = FALSE;
			if ($expectIf && $name === T_IF) {
				$output->delete();
				$output->delete();
				$name = T_ELSEIF;
				$value = 'elseif';
				$catchParenthesis = TRUE;
				$writeSpace = TRUE;
			} else {
				sanitizePreviousWhitespace($output);
				if ($value !== '{') {
					$output->push(NULL, '{');
					$output->push(T_WHITESPACE, PHP_EOL."$indent\t");

					if ($value === ';') {
						list($name, $value) = finishBraceBlock($output, $indent);
					} else {
						$braceAfterSemicolon = TRUE;
					}
				}
			}

		} elseif (isOneLineComment($name, $value)) {
			$value = preg_replace('#^//(\w)#', '// $1', $value);

		} elseif (in_array($name, [T_ARRAY_CAST, T_BOOL_CAST, T_DOUBLE_CAST,
			T_INT_CAST, T_OBJECT_CAST, T_STRING_CAST, T_UNSET_CAST])) {
			$value = str_replace(' ', '', $value);
			$writeSpace = TRUE;

		} elseif ($value === ';') {
			if ($braceAfterSemicolon) {
				$braceAfterSemicolon = FALSE;
				$output->push($name, $value);
				list($name, $value) = finishBraceBlock($output, $indent);
			}
			$searchingFunction = FALSE;

		} elseif ($name === T_STRING && preg_match('/^(?:true|false|null)$/i', $value)) {
			$value = strtoupper($value);

		} elseif (in_array($name, [T_IF, T_ELSEIF, T_ELSE, T_SWITCH, T_DO, T_WHILE, T_FOR, T_FOREACH, T_TRY, T_CATCH])) {
			$indent = getIndent($output->getValue());
			$writeSpace = TRUE;

			if ($name === T_DO) {
				$doWhile = TRUE;
				$expectBrace = TRUE;
			} elseif ($name === T_TRY || $name === T_ELSE) {
				$expectBrace = TRUE;
				$name === T_ELSE && $expectIf = TRUE;
			} elseif (!$doWhile) {
				$catchParenthesis = TRUE;
			}
			if (
				$output->getName(1) !== T_COMMENT
				&& (in_array($name, [T_CATCH, T_ELSEIF, T_ELSE]) || $name === T_WHILE && $doWhile)
			) {
				sanitizePreviousWhitespace($output);
			}

		} elseif ($catchParenthesis) {
			if ($value === '(') {
				$catchParenthesis === TRUE && $catchParenthesis = 0;
				$catchParenthesis++;
			} elseif ($value === ')') {
				$catchParenthesis--;
			}

		} elseif (in_array($name, [T_PUBLIC, T_PROTECTED, T_PRIVATE])) {
			$indent = getIndent($output->getLastNewLineWhitespace());
			$searchingFunction = TRUE;

		} elseif ($name === T_CLASS
			|| $name === T_INTERFACE
			|| $name === T_FUNCTION && $searchingFunction) {
			$name === T_CLASS && $indent = getIndent($output->getLastNewLineWhitespace());
			$braceOnNextLine = TRUE;
			$searchingFunction = FALSE;

		} elseif ($name === T_DOC_COMMENT) {
			$value = sanitizeDocComment($value, getIndent($output->getValue()));

		} elseif ($value === '{') {
			if ($braceOnNextLine) {
				$braceOnNextLine = FALSE;
				sanitizePreviousWhitespace($output, PHP_EOL."$indent");
			} elseif ($output->getName() === T_WHITESPACE) {
				$output->setValue(' ');
			}
		}

		$output->push($name, $value);
	}

	return (string) $output;
}

function sanitizeToken($token) {
	return is_array($token) ? $token : [NULL, $token];
}

function getIndent($whitespace) {
	preg_match('/[\t ]*$/', $whitespace, $m);
	return $m[0];
}

function sanitizePreviousWhitespace(Output $output, $value = ' ') {
	if ($output->getName() === T_WHITESPACE) {
		$output->setValue($value);
	} else {
		$output->push(T_WHITESPACE, $value);
	}
}

function finishBraceBlock(Output $output, $indent) {
	$output->push(T_WHITESPACE, PHP_EOL."$indent");
	return [NULL, '}'];
}

function removeEmptyLines($content) {
	return preg_replace('/\n.*\n.*\n/s', "\n".PHP_EOL, $content);
}

function removeTrailingWhitespace($content) {
	return preg_replace('/[ \t]+$/m', '', $content);
}

function ensureTrailingEol($content) {
	return rtrim($content).PHP_EOL;
}

function isOneLineComment($name, $value) {
	return $name === T_COMMENT && strpos($value, '/*') !== 0;
}

function sanitizeDocComment($value, $indent) {
	$oneLine = !preg_match('/^\/\*\*\s*\n\s*/', $value);
	$value = trim($value, "/*\r\n\t ");
	$lines = explode("\n", trim($value));

	$outputLines = [];
	$table = [];
	for ($i = 0; $i < count($lines)+1; $i++) {
		$last = !isset($lines[$i]);
		if ($last) {
			$line = '';
		} else {
			$line = rtrim($lines[$i], "\t\r ");
			if (preg_match('/^\s*\* ?(.*)/', $line, $m)) {
				$line = $m[1];
			} else {
				$line = ltrim($line);
			}
		}
		if (!$last && preg_match('/^\s*@[a-z-]+/i', $line)) {
			$table[] = preg_split('/\s+/', trim($line));
		} else {
			if ($table) {
				$rows = alignTable($table);
				foreach ($rows as $row) {
					$outputLines[] = $row;
				}
				$table = [];
			}
			!$last && $outputLines[] = $line;
		}
	}
	if ($oneLine && count($outputLines) === 1) {
		return "/** $outputLines[0] */";
	}
	return '/**'.PHP_EOL.implode(PHP_EOL, array_map(function ($line) use($indent) {
		return "$indent * $line";
	}, $outputLines)).PHP_EOL.$indent.' */';
}

function alignTable(array $table) {
	static $annotation_params = [
		'@property' => 2,
		'@property-read' => 2,
		'@property-write' => 2,
		'@var' => 1,
		'@param' => 2,
		'@return' => 1,
	];

	$lines = array_fill(0, count($table), '');
	$widths = [];
	foreach ($table as $row) {
		$i = 0;
		foreach ($row as $col) {
			if ($i == 0) {
				$ceil = 1 + (isset($annotation_params[$col]) ? $annotation_params[$col] : 0);
			} elseif ($i == $ceil) {
				break;
			}
			$w = strlen($col);
			if (!isset($widths[$i]) || $w > $widths[$i]) {
				$widths[$i] = $w;
			}
			$i++;
		}
	}
	$r = 0;
	foreach ($table as $row) {
		$i = 0;
		foreach ($row as $col) {
			if ($i == 0) {
				$ceil = 1 + (isset($annotation_params[$col]) ? $annotation_params[$col] : 0);
			}
			if ($i >= $ceil) {
				$lines[$r] .= "$col ";
			} else {
				$lines[$r] .= str_pad($col, $widths[$i]+1);
			}
			$i++;
		}
		$lines[$r] = trim($lines[$r]);
		$r++;
	}
	return $lines;
}

class Output
{
	private $array = [];

	public function push($name, $value)
	{
		$this->array[] = [$name, $value];
	}

	public function getName($stepsBack = 0)
	{
		if (($token = $this->get($stepsBack)) === NULL) {
			return NULL;
		}
		return $token[0];
	}

	public function getValue($stepsBack = 0)
	{
		if (($token = $this->get($stepsBack)) === NULL) {
			return NULL;
		}
		return $token[1];
	}

	public function setValue($value, $stepsBack = 0)
	{
		if (($index = $this->getIndex($stepsBack)) === NULL) {
			return;
		}
		$this->array[$index][1] = $value;
	}

	private function get($stepsBack)
	{
		if (($i = $this->getIndex($stepsBack)) === NULL) {
			return NULL;
		}
		return $this->array[$i];
	}

	private function getIndex($stepsBack)
	{
		$i = count($this->array) - $stepsBack - 1;
		if ($i < 0) {
			return NULL;
		}
		return $i;
	}

	public function delete($stepsBack = 0)
	{
		$i = $this->getIndex($stepsBack);
		array_splice($this->array, $i, 1);
	}

	public function getLastNewLineWhitespace()
	{
		foreach (array_reverse($this->array) as $token) {
			if ($token[0] === T_WHITESPACE && strpos($token[1], "\n") !== FALSE) {
				return $token[1];
			}
		}
		return NULL;
	}

	public function __tostring()
	{
		$output = '';
		foreach ($this->array as $token) {
			$output .= $token[1];
		}
		return $output;
	}
}

function convertSpacesToTabs($content, $tabWidth = 4) {
	$tokens = token_get_all($content);

	$values = [];
	$eol = FALSE;
	foreach ($tokens as $token) {
		list($name, $value) = sanitizeToken($token);
		if (isOneLineComment($name, $value)) {
			$value = rtrim($value, "\r\n");
			$eol && $value = PHP_EOL.$value;
			$eol = TRUE;
		} elseif ($name === T_WHITESPACE) {
			if ($eol) {
				$value = PHP_EOL.$value;
				$eol = FALSE;
			}
			$value = preg_replace_callback('/^\n[ \t]+/m', function ($m) use ($tabWidth) {
				$chars = count_chars($m[0]);
				$tabs = $chars[9];
				$spaces = $chars[32];
				$tabs += floor($spaces/$tabWidth);
				return "\n".str_repeat("\t", $tabs).str_repeat(' ', $spaces % $tabWidth);
			}, $value);
		} elseif ($eol) {
			$values[] = PHP_EOL;
			$eol = FALSE;
		}
		$values[] = $value;
	}
	return implode('', $values);
}

function orderUseStatements($content) {
	$tokens = token_get_all($content);

	$values = [];
	$inUses = FALSE;
	$enabled = TRUE;
	foreach ($tokens as $token) {
		list($name, $value) = sanitizeToken($token);

		if ($inUses) {
			if ($value === ',' || $value === ';') {
				$value === ';' && $requireUse = TRUE;
				$uses[] = $currentUse;
				$currentUse = '';
			} elseif ($name === T_COMMENT) {
				$comments[count($uses)-1] = $value;

			} elseif ($name === T_WHITESPACE || $requireUse) {
				if ($name === T_WHITESPACE && preg_match('/\n.*\n/', $value)
					|| $name !== T_WHITESPACE && $requireUse && $name !== T_USE) {
					$inUses = FALSE;

					$comments2 = [];
					foreach ($uses as $i => $_) {
						$comments2[] = isset($comments[$i]) ? $comments[$i] : NULL;
					}
					array_multisort($uses, $comments2);
					$block = '';
					foreach ($uses as $i => $use) {
						$use = str_replace(['@', ':'], [' as ', '\\'], $use);
						$block .= "use $use;";
						isset($comments2[$i]) && $block .= " $comments2[$i]";
						$block .= PHP_EOL;
					}
					$values[] = rtrim($block, PHP_EOL);
					$values[] = $value;
				}
				$requireUse = FALSE;
			} elseif ($name !== T_USE) {
				if ($name === T_AS) {
					$currentUse .= '@';
				} elseif ($value === '\\') {
					$currentUse .= ':';
				} else {
					$currentUse .= $value;
				}
			}
		} elseif ($enabled && $name === T_USE) {
			$requireUse = FALSE;
			$inUses = TRUE;
			$uses = [];
			$comments = [];
			$currentUse = '';
		} else {
			if ($name === T_FUNCTION) {
				$enabled = FALSE;
			} elseif ($value === '{') {
				$enabled = TRUE;
			}
			$values[] = $value;
		}
	}
	return implode('', $values);
}

function alignColumns($content) {
	$output = new Output;

	$scanConsts = FALSE;
	$catchConst = FALSE;

	$tokens = token_get_all($content);
	foreach ($tokens as $token) {
		list($name, $value) = sanitizeToken($token);

		if ($scanConsts) {
			if ($catchConst) {
				if ($name === T_CONST) {
					$row[] = $cell;
					$table[] = $row;
					$row = [];
					$cell = '';
					$catchConst = FALSE;
				} elseif (isOneLineComment($name, $value)) {
					$oneLine = TRUE; // kvulí jednořádkovému komentáři a \n
				} elseif ($name !== T_COMMENT
					&& ($name !== T_WHITESPACE || preg_match('/'.($oneLine ? '' : '\n.*').'\n/', $value))) {
					$row[] = $cell;
					$table[] = $row;
					$output->push(NULL, implode(PHP_EOL.$indent, alignTable($table)));
					$output->push(T_WHITESPACE, PHP_EOL.PHP_EOL.getIndent($value));
					$name !== T_WHITESPACE && $output->push($name, $value);
					$scanConsts = FALSE;
				} else {
					$oneLine = FALSE;
				}
			} elseif ($value === '=') {
				$row[] = trim($cell);
				$cell = '';
				$oneLine = FALSE;
			}
			$cell .= $value;
			$value === ';' && $catchConst = TRUE;
		} elseif ($name === T_CONST) {
			$indent = getIndent($output->getValue());
			$scanConsts = TRUE;
			$catchConst = FALSE;
			$oneLine = FALSE;
			$table = $row = [];
			$cell = $value;
		} else {
			$output->push($name, $value);
		}
	}
	return (string) $output;
}
