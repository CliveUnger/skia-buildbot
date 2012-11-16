#!/usr/bin/env python
# Copyright (c) 2012 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

""" Run the Skia GM executable. """

from utils import shell_utils
from build_step import BuildStep
import errno
import os
import shutil
import sys


class RunGM(BuildStep):
  def _PreGM(self,):
    print 'Removing %s' % self._gm_actual_dir
    try:
      shutil.rmtree(self._gm_actual_dir)
    except:
      pass
    print 'Creating %s' % self._gm_actual_dir
    try:
      os.makedirs(self._gm_actual_dir)
    except OSError as e:
      if e.errno == errno.EEXIST:
        pass
      else:
        raise e

  def _RunModulo(self, cmd):
    """ Run GM in multiple concurrent processes using the --modulo flag. """
    subprocesses = []
    retcodes = []
    for idx in range(self._num_cores):
      subprocesses.append(shell_utils.BashAsync(cmd + ['--modulo', str(idx),
                                                       str(self._num_cores)]))
    for proc in subprocesses:
      retcode = 0
      try:
        retcode = shell_utils.LogProcessToCompletion(proc)[0]
      except:
        retcode = 1
      retcodes.append(retcode)
    for retcode in retcodes:
      if retcode != 0:
        raise Exception('Command failed with code %d.' % retcode)

  def _Run(self):
    self._PreGM()
    cmd = [self._PathToBinary('gm'),
           '-w', self._gm_actual_dir,
           ] + self._gm_args
    self._RunModulo(cmd)


if '__main__' == __name__:
  sys.exit(BuildStep.RunBuildStep(RunGM))